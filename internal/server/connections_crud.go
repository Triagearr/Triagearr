package server

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// connectionCRUD is the shared pipeline for DB-owned connection resources
// (arr_connections, torrent_client_connections). It captures the
// list/upsert/delete/test flow once; each resource supplies the kind-checks,
// store ops, codecs, validators and connection-test runner.
type connectionCRUD[Conn any, Input any, DTO any, TestReq any] struct {
	// label appears in error and log messages — e.g. "arr connection",
	// "torrent client connection".
	label string

	knownKind          func(string) bool
	knownKindMsg       string
	implementedKind    func(string) bool      // nil when every known kind is implemented
	implementedKindMsg func(string) string

	list         func(context.Context) ([]Conn, error)
	getByKind    func(context.Context, string) (Conn, error)
	upsert       func(context.Context, Conn) (Conn, error)
	deleteByKind func(context.Context, string) error

	// carryForwardInput preserves a stored secret when the input's secret
	// field is empty — common edit case is "save while keeping the key".
	carryForwardInput func(in *Input, existing Conn)
	validateInput     func(Input) (string, bool)
	inputToConn       func(kind string, in Input) Conn
	connToDTO         func(Conn) DTO

	testKind           func(*TestReq) string
	testURL            func(*TestReq) string
	testTimeoutSeconds func(*TestReq) int
	carryForwardTest   func(req *TestReq, existing Conn) // nil → no carry-forward on Test
	runTest            func(ctx context.Context, req TestReq, timeout time.Duration) error
	defaultTimeoutSecs int

	reload func()
}

func (c *connectionCRUD[Conn, Input, DTO, TestReq]) checkKind(w http.ResponseWriter, kind string) bool {
	if !c.knownKind(kind) {
		writeError(w, http.StatusBadRequest, c.knownKindMsg)
		return false
	}
	return true
}

func (c *connectionCRUD[Conn, Input, DTO, TestReq]) handleList(w http.ResponseWriter, r *http.Request) {
	conns, err := c.list(r.Context())
	if err != nil {
		writeInternal(w, err)
		return
	}
	out := make([]DTO, 0, len(conns))
	for _, conn := range conns {
		out = append(out, c.connToDTO(conn))
	}
	writeJSON(w, http.StatusOK, map[string]any{"connections": out})
}

func (c *connectionCRUD[Conn, Input, DTO, TestReq]) handleUpsert(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !c.checkKind(w, kind) {
		return
	}
	if c.implementedKind != nil && !c.implementedKind(kind) {
		writeError(w, http.StatusBadRequest, c.implementedKindMsg(kind))
		return
	}
	var in Input
	if !decodeJSONBody(w, r, &in) {
		return
	}
	// Carry-forward MUST run before Validate: rejecting an empty secret when
	// a stored one exists would force the operator to re-enter it on every save.
	if existing, err := c.getByKind(r.Context(), kind); err == nil {
		c.carryForwardInput(&in, existing)
	}
	if msg, ok := c.validateInput(in); !ok {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	saved, err := c.upsert(r.Context(), c.inputToConn(kind, in))
	if err != nil {
		writeInternal(w, err)
		return
	}
	c.doReload()
	writeJSON(w, http.StatusOK, c.connToDTO(saved))
}

func (c *connectionCRUD[Conn, Input, DTO, TestReq]) handleDelete(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !c.checkKind(w, kind) {
		return
	}
	if err := c.deleteByKind(r.Context(), kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "no "+c.label+" for kind "+kind)
			return
		}
		writeInternal(w, err)
		return
	}
	c.doReload()
	w.WriteHeader(http.StatusNoContent)
}

func (c *connectionCRUD[Conn, Input, DTO, TestReq]) handleTest(w http.ResponseWriter, r *http.Request) {
	var body TestReq
	if !decodeJSONBody(w, r, &body) {
		return
	}
	kind := c.testKind(&body)
	if !c.knownKind(kind) {
		writeError(w, http.StatusBadRequest, "unknown "+c.label+" kind "+kind)
		return
	}
	if c.testURL(&body) == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if c.carryForwardTest != nil {
		if existing, err := c.getByKind(r.Context(), kind); err == nil {
			c.carryForwardTest(&body, existing)
		}
	}
	secs := c.testTimeoutSeconds(&body)
	if secs == 0 {
		secs = c.defaultTimeoutSecs
	}
	if err := c.runTest(r.Context(), body, time.Duration(secs)*time.Second); err != nil {
		writeError(w, http.StatusBadGateway, "connection test failed: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *connectionCRUD[Conn, Input, DTO, TestReq]) doReload() {
	if c.reload != nil {
		c.reload()
		return
	}
	slog.Warn(c.label + " changed but no Reload hook is wired — registry will not refresh until next SIGHUP")
}
