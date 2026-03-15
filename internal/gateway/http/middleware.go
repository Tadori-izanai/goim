package http

import (
	"fmt"
	"net/http/httputil"
	"runtime"
	"time"

	"github.com/Terry-Mao/goim/internal/gateway/conf"
	"github.com/Terry-Mao/goim/pkg/auth"
	"github.com/gin-gonic/gin"
	log "github.com/golang/glog"
)

func loggerHandler(c *gin.Context) {
	// Start timer
	start := time.Now()
	path := c.Request.URL.Path
	raw := c.Request.URL.RawQuery
	method := c.Request.Method

	// Process request
	c.Next()

	// Stop timer
	end := time.Now()
	latency := end.Sub(start)
	statusCode := c.Writer.Status()
	ecode := c.GetInt(contextErrCode)
	clientIP := c.ClientIP()
	if raw != "" {
		path = path + "?" + raw
	}
	log.Infof("METHOD:%s | PATH:%s | CODE:%d | IP:%s | TIME:%d | ECODE:%d", method, path, statusCode, clientIP, latency/time.Millisecond, ecode)
}

func recoverHandler(c *gin.Context) {
	defer func() {
		if err := recover(); err != nil {
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			httprequest, _ := httputil.DumpRequest(c.Request, false)
			pnc := fmt.Sprintf("[Recovery] %s panic recovered:\n%s\n%s\n%s", time.Now().Format("2006-01-02 15:04:05"), string(httprequest), err, buf)
			fmt.Print(pnc)
			log.Error(pnc)
			c.AbortWithStatus(500)
		}
	}()
	c.Next()
}

const contextMid = "context/mid"

func jwtHandler(c *gin.Context) {
	header := c.GetHeader("Authorization")
	if header == "" || len(header) < 8 || header[:7] != "Bearer " {
		c.AbortWithStatusJSON(401, resp{Code: -401, Message: "missing or invalid Authorization header"})
		return
	}
	tokenStr := header[7:]

	claims, err := auth.ParseToken(conf.Conf.JWT.Secret, tokenStr)
	if err != nil {
		c.AbortWithStatusJSON(401, resp{Code: -401, Message: "invalid or expired token"})
		return
	}

	c.Set(contextMid, claims.Mid)
	c.Next()
}

func getUserIDFromBearer(c *gin.Context) (int64, bool) {
	val, ok := c.Get(contextMid)
	if !ok {
		return 0, false
	}
	userID := val.(int64)
	return userID, true
}
