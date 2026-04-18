// Copyright (c) 2023 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

// Package service is a scaffolded placeholder. The template's guild-specific
// handlers and CloudSave storage were stripped during M1 phase 1; real
// playtesthub RPCs land in M1 phase 2 when the `playtesthub.v1` proto is
// introduced.
package service

import (
	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/repository"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb"
)

type MyServiceServerImpl struct {
	pb.UnimplementedServiceServer
	tokenRepo   repository.TokenRepository
	configRepo  repository.ConfigRepository
	refreshRepo repository.RefreshTokenRepository
}

func NewMyServiceServer(
	tokenRepo repository.TokenRepository,
	configRepo repository.ConfigRepository,
	refreshRepo repository.RefreshTokenRepository,
) *MyServiceServerImpl {
	return &MyServiceServerImpl{
		tokenRepo:   tokenRepo,
		configRepo:  configRepo,
		refreshRepo: refreshRepo,
	}
}
