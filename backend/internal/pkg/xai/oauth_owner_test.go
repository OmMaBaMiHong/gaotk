package xai

import (
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestSessionOwnerSurvivesRedisRoundTrip(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	writer := NewRedisSessionStore(client)
	defer writer.Stop()
	reader := NewRedisSessionStore(client)
	defer reader.Stop()
	writer.Set("owned", &OAuthSession{OwnerUserID: 42, State: "state", CreatedAt: time.Now()})
	session, ok := reader.Get("owned")
	require.True(t, ok)
	require.Equal(t, int64(42), session.OwnerUserID)
}
