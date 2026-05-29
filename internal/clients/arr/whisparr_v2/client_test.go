package whisparr_v2_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	whisparr_v2 "github.com/Triagearr/Triagearr/internal/clients/arr/whisparr_v2"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestNew_WiresWhisparrV2Type(t *testing.T) {
	c, err := whisparr_v2.New(whisparr_v2.Options{Name: "whisparr", BaseURL: "http://whisparr:6969", Poll: true})
	require.NoError(t, err)
	require.Equal(t, triagearr.ArrTypeWhisparrV2, c.Type())
	require.Equal(t, "whisparr", c.Name())
	require.True(t, c.Poll())
}
