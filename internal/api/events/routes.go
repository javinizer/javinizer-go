package events

import (
	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
)

func RegisterRoutes(protected *gin.RouterGroup, eventRepo database.EventRepositoryInterface) {
	protected.GET("/events", listEvents(eventRepo))
	protected.GET("/events/stats", eventStats(eventRepo))
	protected.DELETE("/events", deleteEvents(eventRepo))
}
