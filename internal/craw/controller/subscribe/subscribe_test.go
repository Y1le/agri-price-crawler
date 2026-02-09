package subscribe

import (
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"

	srvv1 "github.com/Y1le/agri-price-crawler/internal/craw/service/v1"
	"github.com/Y1le/agri-price-crawler/internal/craw/store"
)

func TestNewSubscribeController(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockFactory := store.NewMockFactory(ctrl)

	type args struct {
		store store.Factory
	}
	tests := []struct {
		name string
		args args
		want *SubscribeController
	}{
		{
			name: "default",
			args: args{
				store: mockFactory,
			},
			want: &SubscribeController{
				srv: srvv1.NewService(mockFactory),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewSubscribeController(tt.args.store); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewSubscribeController() = %v, want %v", got, tt.want)
			}
		})
	}
}
