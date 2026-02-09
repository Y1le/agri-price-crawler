package hnprice

import (
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"

	srvv1 "github.com/Y1le/agri-price-crawler/internal/craw/service/v1"
	"github.com/Y1le/agri-price-crawler/internal/craw/store"
)

func TestNewHNPriceController(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockFactory := store.NewMockFactory(ctrl)

	type args struct {
		store store.Factory
	}
	tests := []struct {
		name string
		args args
		want *HNPriceController
	}{
		{
			name: "default",
			args: args{
				store: mockFactory,
			},
			want: &HNPriceController{
				srv: srvv1.NewService(mockFactory),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewHNPriceController(tt.args.store); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewHNPriceController() = %v, want %v", got, tt.want)
			}
		})
	}
}
