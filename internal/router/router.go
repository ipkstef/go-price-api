package router

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"go-price-api/internal/handlers"
	"go-price-api/internal/middleware"
)

func maxBodySize(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		}
		c.Next()
	}
}

func Setup(pool *pgxpool.Pool, jwtSecret string, jwtExpiry time.Duration) *gin.Engine {
	r := gin.Default()
	r.MaxMultipartMemory = 1 << 20 // 1 MB
	r.Use(maxBodySize(1 << 20))    // 1 MB

	auth := &handlers.AuthHandler{DB: pool, JWTSecret: jwtSecret, JWTExpiry: jwtExpiry}
	products := &handlers.ProductHandler{DB: pool}
	groups := &handlers.GroupHandler{DB: pool}
	prices := &handlers.PriceHandler{DB: pool}
	reference := &handlers.ReferenceHandler{DB: pool}
	ingestion := &handlers.IngestionHandler{DB: pool}
	docs := &handlers.DocsHandler{}

	r.GET("/", docs.Index)

	public := r.Group("/auth")
	{
		public.POST("/register", auth.Register)
		public.POST("/login", auth.Login)
	}

	api := r.Group("/")
	api.Use(middleware.Auth(jwtSecret))
	{
		api.POST("/auth/refresh", auth.Refresh)

		api.GET("/products", products.List)
		api.GET("/products/:id", products.Get)
		api.GET("/products/:id/skus", products.SKUs)
		api.GET("/products/:id/prices", products.Prices)
		api.GET("/products/:id/prices/history", products.PriceHistory)

		api.GET("/groups", groups.List)
		api.GET("/groups/:id", groups.Get)
		api.GET("/groups/:id/products", groups.Products)

		api.POST("/prices/latest", prices.BulkLatest)
		api.GET("/prices/movers", prices.Movers)

		api.GET("/conditions", reference.Conditions)
		api.GET("/languages", reference.Languages)
		api.GET("/printings", reference.Printings)
		api.GET("/rarities", reference.Rarities)

		api.GET("/ingestion/runs", ingestion.List)
		api.GET("/ingestion/runs/:id", ingestion.Get)
	}

	return r
}
