package events

import "github.com/gin-gonic/gin"

func RegisterRoutes(protected *gin.RouterGroup, deps *ServerDependencies) {
	protected.GET("/events", listEvents(deps))
	protected.GET("/events/stats", eventStats(deps))
	protected.DELETE("/events", deleteEvents(deps))
}
