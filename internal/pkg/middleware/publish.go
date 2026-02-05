package middleware

import (
	"github.com/gin-gonic/gin"
)

// Publish publish a redis event to specified redis channel when some action occurred.
func Publish() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		// if c.Writer.Status() != http.StatusOK {
		// 	log.L(c).Debugf("request failed with http status code `%d`, ignore publish message", c.Writer.Status())

		// 	return
		// }

		// var resource string

		// pathSplit := strings.Split(c.Request.URL.Path, "/")
		// if len(pathSplit) > 2 {
		// 	resource = pathSplit[2]
		// }

		// method := c.Request.Method

		// switch resource {
		// case "policies":
		// 	notify(c, method, load.NoticePolicyChanged)
		// case "secrets":
		// 	notify(c, method, load.NoticeSecretChanged)
		// default:
		// }
	}
}

// func notify(ctx context.Context, method string, command load.NotificationCommand) {
// switch method {
// case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
// 	redisStore := &storage.RedisCluster{}
// 	message, _ := json.Marshal(load.Notification{Command: command})

// 	if err := redisStore.Publish(load.RedisPubSubChannel, string(message)); err != nil {
// 		log.L(ctx).Errorw("publish redis message failed", "error", err.Error())
// 	}
// 	log.L(ctx).Debugw("publish redis message", "method", method, "command", command)
// default:
// }
// }
