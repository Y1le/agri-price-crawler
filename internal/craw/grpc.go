package craw

import (
	"net"

	"github.com/Y1le/agri-price-crawler/pkg/log"
	"google.golang.org/grpc"
)

type grpcServer struct {
	*grpc.Server
	address string
}

func (s *grpcServer) Run() {
	log.Debugf("grpc server address: %s", s.address)
	listen, err := net.Listen("tcp", s.address)
	if err != nil {
		log.Fatalf("failed to listen: %s", err.Error())
	}

	go func() {
		if err := s.Serve(listen); err != nil {
			log.Fatalf("failed to run server at %s", s.address)
		}
	}()

	log.Infof("grpc server started at %s", s.address)
}

func (s *grpcServer) Close() {
	s.GracefulStop()
	log.Infof("GRPC server on %s stopped", s.address)
}
