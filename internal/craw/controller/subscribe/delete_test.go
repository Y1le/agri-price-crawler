package subscribe

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang/mock/gomock"

	srvv1 "github.com/Y1le/agri-price-crawler/internal/craw/service/v1"
)

func TestSubscribeController_Delete(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := srvv1.NewMockService(ctrl)
	mockSubscribeSrv := srvv1.NewMockSubscribeSrv(ctrl)
	mockSubscribeSrv.EXPECT().Delete(gomock.Any(), gomock.Eq("admin"), gomock.Any()).Return(nil)
	mockService.EXPECT().Subscribes().Return(mockSubscribeSrv)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request, _ = http.NewRequest("DELETE", "/v1/subscribes", nil)
	c.Params = []gin.Param{{Key: "email", Value: "admin"}}

	type fields struct {
		srv srvv1.Service
	}
	type args struct {
		c *gin.Context
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name: "default",
			fields: fields{
				srv: mockService,
			},
			args: args{
				c: c,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SubscribeController{
				srv: tt.fields.srv,
			}
			s.Delete(tt.args.c)
		})
	}
}
