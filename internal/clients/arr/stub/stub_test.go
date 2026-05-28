package stub_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/arr/stub"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestStub_IdentityAndFlags(t *testing.T) {
	c, err := stub.New(stub.Options{
		Name: "main", Type: triagearr.ArrTypeLidarr, BaseURL: "http://x",
		Poll: true, Act: false,
	})
	require.NoError(t, err)
	require.Equal(t, "main", c.Name())
	require.Equal(t, triagearr.ArrTypeLidarr, c.Type())
	require.True(t, c.Poll())
	require.False(t, c.Act())
}

func TestStub_AllOperationalMethodsErrorOut(t *testing.T) {
	c, err := stub.New(stub.Options{Name: "main", Type: triagearr.ArrTypeReadarr})
	require.NoError(t, err)
	ctx := context.Background()

	require.ErrorContains(t, c.HealthCheck(ctx), "not implemented in M1")

	items, err := c.ListMedia(ctx)
	require.Nil(t, items)
	require.ErrorContains(t, err, "ListMedia not implemented in M1")

	// Stubs deliberately do not implement FileDeleter — the type-assert in
	// the registry/actor path is the gate, not a stub error message.
	var _ triagearr.ArrInstance = c
	_, isDeleter := any(c).(triagearr.FileDeleter)
	require.False(t, isDeleter)
}

func TestStub_Validations(t *testing.T) {
	_, err := stub.New(stub.Options{})
	require.Error(t, err)
	_, err = stub.New(stub.Options{Name: "x"})
	require.Error(t, err)
}
