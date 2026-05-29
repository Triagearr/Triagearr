package readarr_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/arr/readarr"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestNew_WiresReadarrType(t *testing.T) {
	c, err := readarr.New(readarr.Options{Name: "books", BaseURL: "http://readarr:8787", Poll: true, Act: true})
	require.NoError(t, err)
	require.Equal(t, triagearr.ArrTypeReadarr, c.Type())
	require.Equal(t, "books", c.Name())
	require.True(t, c.Poll())
	require.True(t, c.Act())
}
