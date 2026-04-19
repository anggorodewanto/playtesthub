package service

import (
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

// PlaytesthubServiceServer is the gRPC handler for the playtesthub.v1
// service. Embedding UnimplementedPlaytesthubServiceServer makes every
// method return codes.Unimplemented until a concrete handler lands in M1
// phases 6 (playtest CRUD) and 7 (signup + applicant).
type PlaytesthubServiceServer struct {
	pb.UnimplementedPlaytesthubServiceServer
}

func NewPlaytesthubServiceServer() *PlaytesthubServiceServer {
	return &PlaytesthubServiceServer{}
}
