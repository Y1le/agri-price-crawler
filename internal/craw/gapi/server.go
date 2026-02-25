package gapi

import (
	"context"

	"github.com/Y1le/agri-price-crawler/internal/craw/store"
	"github.com/Y1le/agri-price-crawler/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// crawService 是 CrawService gRPC 服务的实际实现
type crawService struct {
	pb.UnimplementedCrawServiceServer               // 必须嵌入这个结构体以保证向前兼容
	store                             store.Factory // 注入存储层依赖
}

// NewCrawService 创建 CrawService 实例
func NewCrawService(store store.Factory) pb.CrawServiceServer {
	return &crawService{
		store: store,
	}
}

// CreateUser 实现创建用户的 gRPC 方法
func (s *crawService) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.CreateUserResponse, error) {
	// 这里实现你的业务逻辑
	// 示例：简单的参数校验和存储操作
	if req.Username == "" || req.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "用户名和密码不能为空")
	}

	// 调用存储层创建用户（需要你根据实际的 store 接口实现）
	// userID, err := s.store.CreateUser(ctx, req.Username, req.Password)
	// if err != nil {
	//     return nil, status.Error(codes.Internal, "创建用户失败: " + err.Error())
	// }

	// 返回成功响应
	return &pb.CreateUserResponse{
		User: &pb.User{
			Username: req.Username,
		},
	}, nil
}

// LoginUser 实现用户登录的 gRPC 方法
func (s *crawService) LoginUser(ctx context.Context, req *pb.LoginUserRequest) (*pb.LoginUserResponse, error) {

	// 示例：验证用户信息
	// user, err := s.store.GetUserByUsername(ctx, req.Username)
	// if err != nil {
	//     return nil, status.Error(codes.NotFound, "用户不存在")
	// }
	// if user.Password != req.Password { // 注意：实际项目中要使用加密对比
	//     return nil, status.Error(codes.Unauthenticated, "密码错误")
	// }

	return &pb.LoginUserResponse{

		Token: "fake-jwt-token-123456", // 示例 Token
	}, nil
}
