// Copyright (c) 2023-2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package common

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
)

type Gateway struct {
	mux      *runtime.ServeMux
	basePath string
}

// forwardCookieHeaderMatcher augments the grpc-gateway default incoming-header
// matcher so the raw HTTP Cookie header is forwarded as gRPC metadata. The
// auth interceptor uses it to extract the `access_token` cookie when the AGS
// Admin Portal renders this app as an Extend App UI — the host ships the
// token via httpOnly cookie, not an Authorization header, so the cookie path
// is the primary auth signal for admin RPCs served to the embedded UI.
func forwardCookieHeaderMatcher(key string) (string, bool) {
	if strings.EqualFold(key, "cookie") {
		return "cookie", true
	}
	return runtime.DefaultHeaderMatcher(key)
}

func NewGateway(ctx context.Context, grpcServerEndpoint string, basePath string) (*Gateway, error) {
	mux := runtime.NewServeMux(runtime.WithIncomingHeaderMatcher(forwardCookieHeaderMatcher))
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	err := pb.RegisterPlaytesthubServiceHandlerFromEndpoint(ctx, mux, grpcServerEndpoint, opts)
	if err != nil {
		return nil, err
	}

	return &Gateway{
		mux:      mux,
		basePath: basePath,
	}, nil
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Strip the base path, since the base_path configuration in protofile won't actually do the routing
	// Reference: https://github.com/grpc-ecosystem/grpc-gateway/pull/919/commits/1c34df861cfc0d6cb19ea617921d7d9eaa209977
	http.StripPrefix(g.basePath, g.mux).ServeHTTP(w, r)
}
