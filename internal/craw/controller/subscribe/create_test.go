package subscribe

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang/mock/gomock"

	srvv1 "github.com/Y1le/agri-price-crawler/internal/craw/service/v1"
)

func TestSubscribeController_Create(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockService := srvv1.NewMockService(ctrl)
	mockSubscribeSrv := srvv1.NewMockSubscribeSrv(ctrl)

	mockSubscribeSrv.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	mockService.EXPECT().Subscribes().Return(mockSubscribeSrv)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	body := bytes.NewBufferString(
		`{"metadata":{"name":"admin"},"email":"aaa@qq.com","city":"Beijing"}`,
	)
	c.Request, _ = http.NewRequest("POST", "/v1/subscribes", body)
	c.Request.Header.Set("Content-Type", "application/json")

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
			name: "success_create_subscribe",
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
			s.Create(tt.args.c)
		})
	}
}
