package lidarr_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Triagearr/Triagearr/internal/clients/arr/lidarr"
	"github.com/Triagearr/Triagearr/internal/triagearr"
)

func TestNew_WiresLidarrType(t *testing.T) {
	c, err := lidarr.New(lidarr.Options{Name: "music", BaseURL: "http://lidarr:8686", Poll: true, Act: false})
	require.NoError(t, err)
	require.Equal(t, triagearr.ArrTypeLidarr, c.Type())
	require.Equal(t, "music", c.Name())
	require.True(t, c.Poll())
	require.False(t, c.Act())
}
