package whisparr_v3_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	whisparr_v3 "github.com/Triagearr/Triagearr/internal/clients/arr/whisparr_v3"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestNew_WiresWhisparrV3Type(t *testing.T) {
	c, err := whisparr_v3.New(whisparr_v3.Options{Name: "whisparr3", BaseURL: "http://whisparr:6969", Poll: true})
	require.NoError(t, err)
	require.Equal(t, triagearr.ArrTypeWhisparrV3, c.Type())
	require.Equal(t, "whisparr3", c.Name())
	require.True(t, c.Poll())
}
