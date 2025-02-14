// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package daemon

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/hashicorp/boundary/api/authtokens"
	"github.com/hashicorp/boundary/api/sessions"
	"github.com/hashicorp/boundary/api/targets"
	"github.com/hashicorp/boundary/internal/clientcache/internal/cache"
	"github.com/stretchr/testify/require"
)

type TestServer struct {
	*CacheServer
	dotDir string
	cmd    Commander
}

// NewTestServer creates a test cache server using reasonable defaults for
// tests.  Supports the option WithDebugFlag to enable debug output for sql
func NewTestServer(t *testing.T, cmd Commander, opt ...Option) *TestServer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	opts, err := getOpts(opt...)
	require.NoError(t, err)
	dotDir := t.TempDir()

	cfg := &Config{
		ContextCancel:          cancel,
		RefreshInterval:        DefaultRefreshInterval,
		RecheckSupportInterval: DefaultRecheckSupportInterval,
		StoreDebug:             opts.withDebug,
		LogWriter:              io.Discard,
		DotDirectory:           dotDir,
	}

	s, err := New(ctx, cfg)
	require.NoError(t, err)
	return &TestServer{
		CacheServer: s,
		dotDir:      dotDir,
		cmd:         cmd,
	}
}

// BaseDotDir returns the base directory in which the daemon socket is
// created.
func (s *TestServer) BaseDotDir() string {
	return s.dotDir
}

// Serve runs the cache server. This is a blocking call and returns when the
// server is shutdown or stops for any other reason.
func (s *TestServer) Serve(t *testing.T, opt ...Option) error {
	t.Helper()
	ctx := context.Background()

	t.Cleanup(func() {
		s.Shutdown(ctx)
	})
	return s.CacheServer.Serve(ctx, s.cmd, opt...)
}

func (s *TestServer) AddKeyringToken(t *testing.T, address, keyring, tokenName, tokenId string, atReadFn cache.BoundaryTokenReaderFn) {
	t.Helper()
	ctx := context.Background()
	r, err := cache.NewRepository(ctx, s.CacheServer.store, &sync.Map{}, s.cmd.ReadTokenFromKeyring, atReadFn)
	require.NoError(t, err)

	require.NoError(t, r.AddKeyringToken(ctx, address, cache.KeyringToken{
		KeyringType: keyring,
		TokenName:   tokenName,
		AuthTokenId: tokenId,
	}))
}

// AddResources adds targets to the cache for the provided address, token name,
// and keyring type. They token info must already be known to the server.
func (s *TestServer) AddResources(t *testing.T, p *authtokens.AuthToken, tars []*targets.Target, sess []*sessions.Session, atReadFn cache.BoundaryTokenReaderFn) {
	t.Helper()
	ctx := context.Background()
	r, err := cache.NewRepository(ctx, s.CacheServer.store, &sync.Map{}, s.cmd.ReadTokenFromKeyring, atReadFn)
	require.NoError(t, err)

	tarFn := func(ctx context.Context, _, tok string, _ cache.RefreshTokenValue) ([]*targets.Target, []string, cache.RefreshTokenValue, error) {
		if tok != p.Token {
			return nil, nil, "", nil
		}
		return tars, nil, "addedtargets", nil
	}
	sessFn := func(ctx context.Context, _, tok string, _ cache.RefreshTokenValue) ([]*sessions.Session, []string, cache.RefreshTokenValue, error) {
		if tok != p.Token {
			return nil, nil, "", nil
		}
		return sess, nil, "addedsessions", nil
	}
	rs, err := cache.NewRefreshService(ctx, r, 0, 0)
	require.NoError(t, err)
	require.NoError(t, rs.Refresh(ctx, cache.WithTargetRetrievalFunc(tarFn), cache.WithSessionRetrievalFunc(sessFn)))
}

// AddUnsupportedCachingData provides data in a way that simulates it coming from
// a boundary instance that does not support refresh tokens. Since refresh tokens
// are required for caching, this has the effect of the data not being cached and
// the user being identified as not supported in the cache.
func (s *TestServer) AddUnsupportedCachingData(t *testing.T, p *authtokens.AuthToken, atReadFn cache.BoundaryTokenReaderFn) {
	t.Helper()
	ctx := context.Background()
	r, err := cache.NewRepository(ctx, s.CacheServer.store, &sync.Map{}, s.cmd.ReadTokenFromKeyring, atReadFn)
	require.NoError(t, err)

	tarFn := func(ctx context.Context, _, tok string, _ cache.RefreshTokenValue) ([]*targets.Target, []string, cache.RefreshTokenValue, error) {
		if tok != p.Token {
			return nil, nil, "", nil
		}
		return []*targets.Target{
			{Id: "ttcp_unsupported", Name: "unsupported", Description: "not supported"},
		}, nil, "", cache.ErrRefreshNotSupported
	}
	sessFn := func(ctx context.Context, _, tok string, _ cache.RefreshTokenValue) ([]*sessions.Session, []string, cache.RefreshTokenValue, error) {
		if tok != p.Token {
			return nil, nil, "", nil
		}
		return []*sessions.Session{}, nil, "", cache.ErrRefreshNotSupported
	}
	rs, err := cache.NewRefreshService(ctx, r, 0, 0)
	require.NoError(t, err)
	err = rs.Refresh(ctx, cache.WithTargetRetrievalFunc(tarFn), cache.WithSessionRetrievalFunc(sessFn))
	require.ErrorContains(t, err, "not supported for this controller")
}
