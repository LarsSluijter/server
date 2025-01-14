package router

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gotify/location"
	"github.com/gotify/server/api"
	"github.com/gotify/server/api/stream"
	"github.com/gotify/server/auth"
	"github.com/gotify/server/config"
	"github.com/gotify/server/database"
	"github.com/gotify/server/docs"
	"github.com/gotify/server/error"
	"github.com/gotify/server/mode"
	"github.com/gotify/server/model"
	"github.com/gotify/server/plugin"
	"github.com/gotify/server/ui"
)

// Create creates the gin engine with all routes.
func Create(db *database.GormDatabase, vInfo *model.VersionInfo, conf *config.Configuration) (*gin.Engine, func()) {
	g := gin.New()

	g.Use(gin.Logger(), gin.Recovery(), error.Handler(), location.Default())
	g.NoRoute(error.NotFound())

	streamHandler := stream.New(200*time.Second, 15*time.Second, conf.Server.Stream.AllowedOrigins)
	authentication := auth.Auth{DB: db}
	messageHandler := api.MessageAPI{Notifier: streamHandler, DB: db}
	clientHandler := api.ClientAPI{
		DB:            db,
		ImageDir:      conf.UploadedImagesDir,
		NotifyDeleted: streamHandler.NotifyDeletedClient,
	}
	applicationHandler := api.ApplicationAPI{
		DB:       db,
		ImageDir: conf.UploadedImagesDir,
	}
	userChangeNotifier := new(api.UserChangeNotifier)
	userHandler := api.UserAPI{DB: db, PasswordStrength: conf.PassStrength, UserChangeNotifier: userChangeNotifier}

	pluginManager, err := plugin.NewManager(db, conf.PluginsDir, g.Group("/plugin/:id/custom/"), streamHandler)
	if err != nil {
		panic(err)
	}
	pluginHandler := api.PluginAPI{
		Manager:  pluginManager,
		Notifier: streamHandler,
		DB:       db,
	}

	userChangeNotifier.OnUserDeleted(streamHandler.NotifyDeletedUser)
	userChangeNotifier.OnUserDeleted(pluginManager.RemoveUser)
	userChangeNotifier.OnUserAdded(pluginManager.InitializeForUserID)

	ui.Register(g)

	g.GET("/swagger", docs.Serve)
	g.Static("/image", conf.UploadedImagesDir)
	g.GET("/docs", docs.UI)

	g.Use(func(ctx *gin.Context) {
		ctx.Header("Content-Type", "application/json")

		if mode.IsDev() {
			ctx.Header("Access-Control-Allow-Origin", "*")
			ctx.Header("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS,PUT")
			ctx.Header("Access-Control-Allow-Headers", "X-Gotify-Key,Authorization,Content-Type,Upgrade,Origin,Connection,Accept-Encoding,Accept-Language,Host")
		}

		for header, value := range conf.Server.ResponseHeaders {
			ctx.Header(header, value)
		}
	})

	{
		g.GET("/plugin", authentication.RequireClient(), pluginHandler.GetPlugins)
		pluginRoute := g.Group("/plugin/", authentication.RequireClient())
		{
			pluginRoute.GET("/:id/config", pluginHandler.GetConfig)
			pluginRoute.POST("/:id/config", pluginHandler.UpdateConfig)
			pluginRoute.GET("/:id/display", pluginHandler.GetDisplay)
			pluginRoute.POST("/:id/enable", pluginHandler.EnablePlugin)
			pluginRoute.POST("/:id/disable", pluginHandler.DisablePlugin)
		}
	}

	g.OPTIONS("/*any")

	// swagger:operation GET /version version getVersion
	//
	// Get version information.
	//
	// ---
	// produces: [application/json]
	// responses:
	//   200:
	//     description: Ok
	//     schema:
	//         $ref: "#/definitions/VersionInfo"
	g.GET("version", func(ctx *gin.Context) {
		ctx.JSON(200, vInfo)
	})

	g.Group("/").Use(authentication.RequireApplicationToken()).POST("/message", messageHandler.CreateMessage)

	clientAuth := g.Group("")
	{
		clientAuth.Use(authentication.RequireClient())
		app := clientAuth.Group("/application")
		{

			app.GET("", applicationHandler.GetApplications)

			app.POST("", applicationHandler.CreateApplication)

			app.POST("/:id/image", applicationHandler.UploadApplicationImage)

			app.PUT("/:id", applicationHandler.UpdateApplication)

			app.DELETE("/:id", applicationHandler.DeleteApplication)

			tokenMessage := app.Group("/:id/message")
			{

				tokenMessage.GET("", messageHandler.GetMessagesWithApplication)

				tokenMessage.DELETE("", messageHandler.DeleteMessageWithApplication)
			}
		}

		client := clientAuth.Group("/client")
		{

			client.GET("", clientHandler.GetClients)

			client.POST("", clientHandler.CreateClient)

			client.DELETE("/:id", clientHandler.DeleteClient)

			client.PUT("/:id", clientHandler.UpdateClient)
		}

		message := clientAuth.Group("/message")
		{

			message.GET("", messageHandler.GetMessages)

			message.DELETE("", messageHandler.DeleteMessages)

			message.DELETE("/:id", messageHandler.DeleteMessage)

			message.PUT("/:id", messageHandler.UpdateMessage)
		}

		clientAuth.GET("/stream", streamHandler.Handle)

		clientAuth.GET("current/user", userHandler.GetCurrentUser)

		clientAuth.POST("current/user/password", userHandler.ChangePassword)
	}

	authAdmin := g.Group("/user")
	{
		authAdmin.Use(authentication.RequireAdmin())

		authAdmin.GET("", userHandler.GetUsers)

		authAdmin.POST("", userHandler.CreateUser)

		authAdmin.DELETE("/:id", userHandler.DeleteUserByID)

		authAdmin.GET("/:id", userHandler.GetUserByID)

		authAdmin.POST("/:id", userHandler.UpdateUserByID)
	}
	return g, streamHandler.Close
}
