// CRM Dashboard — backend.
// Internal service.

package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	"wa-assistant/backend/config"
	"wa-assistant/backend/database"
	"wa-assistant/backend/handlers"
	"wa-assistant/backend/license"
	"wa-assistant/backend/models"
	"wa-assistant/backend/services"
	"wa-assistant/backend/ui"

	"github.com/gin-gonic/gin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "license-reset" {
		if err := license.Reset(); err != nil {
			log.Fatalf("Reset lisensi gagal: %v", err)
		}
		log.Println("Lisensi berhasil di-reset. Jalankan aplikasi kembali untuk aktivasi di mesin ini.")
		return
	}

	database.Init()
	handlers.ConsolidateAllKnowledge()

	// Verifikasi lisensi saat startup (senyap: tanpa key, server tetap jalan;
	// dengan key yang tidak valid, hanya dicatat — tidak menghentikan layanan
	// dan tidak menampilkan apa pun ke pengguna).
	license.Verify()
	appCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// A terminal license decision or expired offline grace triggers the same
	// graceful shutdown path as SIGTERM.
	license.StartHeartbeat(appCtx, 6*time.Hour, 12*time.Hour, func(message string) {
		log.Printf("Shutdown karena lisensi: %s", message)
		stop()
	})

	services.InitAI()
	services.InitEmbedding()

	services.Go("BackfillEmbeddings", services.BackfillEmbeddings)
	services.InitWA(config.Env("DB_PATH", "./wa-assistant.db"))
	services.SetHandlers(handlers.OnWAMessage, handlers.OnDeviceLinked)
	services.SetOutgoingMessageHandler(handlers.OnWAOwnMessage)
	services.SetLabelHandlers(handlers.OnLabelEdit, handlers.OnLabelAssoc)
	services.SetConnectedHandler(handlers.OnAgentConnected)
	services.SetReceiptHandler(handlers.OnWAReceipt)
	handlers.InitGroupGuard()

	// Sambungkan ulang semua agent yang sudah ter-link.
	services.Go("StartAgents", handlers.StartAgents)
	handlers.CleanupBroadcastJunk()     // hapus thread sistem @broadcast/@newsletter yang bocor
	handlers.CleanupOrphanAssignments() // hapus relasi CS-agent yang sudah tidak valid
	services.SetHistorySyncHandler(handlers.OnWAHistorySync)
	services.SetHistoryChatStateHandler(handlers.OnWAHistoryChatState)
	services.SetWhatsAppReadStateHandler(handlers.OnWAWhatsAppReadState)
	services.SetMessageRevokeHandler(handlers.OnWAMessageRevoke)
	services.SetChatPresenceHandler(handlers.OnWAChatPresence)
	services.StartReconnectWatchdogCtx(appCtx, 90*time.Second)
	// Gabungkan pengirim yang terlanjur tercatat sebagai LID ke nomor telepon aslinya.
	handlers.StartLIDSweeperCtx(appCtx)

	// Lanjutkan broadcast yang sempat terhenti saat server mati; tandai jadwal yang nyangkut.
	services.Go("ResumeBroadcasts", handlers.ResumeBroadcasts)
	handlers.CleanupStuckSchedules()

	// Seed daftar kota RajaOngkir ke DB lokal (async, non-blocking).
	services.Go("SeedShippingCities", services.SeedShippingCities)

	// Scheduler pesan terjadwal + pembersihan media lama.
	handlers.StartSchedulerCtx(appCtx)
	services.Go("LearningScheduler", func() { handlers.StartLearningScheduler(appCtx) })
	handlers.StartMediaCleanup(config.EnvInt("MEDIA_RETENTION_DAYS", 30))
	// Retry pesan WhatsApp yang gagal terkirim.
	handlers.StartFailedSendRetry(appCtx)
	// Tracking sinkronisasi otomatis pesanan pengiriman (Mengantar).
	handlers.StartShippingTrackingSync()
	// Meta CAPI tidak digunakan — instalasi internal.

	// Bersihkan entry throttle login yang kadaluarsa secara berkala.
	handlers.StartLoginThrottleSweeper()

	r := gin.Default()
	maxRequestMB := config.EnvInt("MAX_REQUEST_MB", 32)
	r.MaxMultipartMemory = int64(config.EnvInt("MAX_MULTIPART_MEMORY_MB", 16)) << 20
	r.Use(handlers.BodySizeLimit(int64(maxRequestMB)<<20), handlers.CORS())

	api := r.Group("/api")
	{
		api.POST("/login", handlers.Login)
		api.GET("/verify-email", handlers.VerifyEmail)
		api.POST("/resend-verification", handlers.ResendVerification)
		api.POST("/forgot-password", handlers.ForgotPassword)
		api.POST("/reset-password", handlers.ResetPassword)
		api.GET("/agents/:id/media/:cid", handlers.ServeMedia)
		api.GET("/agents/:id/products/:pid/image", handlers.ServeProductImage)
		api.GET("/agents/:id/media-assets/:assetId/file", handlers.ServeMediaAssetFile)
		api.GET("/agents/:id/profile-picture", handlers.ServeProfilePicture)
		api.GET("/me", handlers.AuthMiddleware(), handlers.Me)
		api.PUT("/profile", handlers.AuthMiddleware(), handlers.UpdateProfile)
		api.PUT("/change-password", handlers.AuthMiddleware(), handlers.ChangePassword)
		// Default jeda blast per akun. Terbuka untuk semua role: tiap orang mengatur
		// ritme kirimnya sendiri (id user diambil dari token, bukan dari body).
		// Nilainya ikut terkirim di GET /me sebagai "blast_delay".
		api.PUT("/blast-delay", handlers.AuthMiddleware(), handlers.SaveBlastDelay)

		// REST API publik (autentikasi API key per-nomor) untuk integrasi eksternal.
		v1 := api.Group("/v1", handlers.APIKeyMiddleware())
		{
			v1.POST("/messages", handlers.APISendMessage)
			v1.GET("/messages/:message_id/media", handlers.APIServeMessageMedia)
			v1.GET("/messages/:message_id/analysis", handlers.APIMessageAnalysis)
			v1.POST("/otp/request", handlers.APIRequestOTP)
			v1.POST("/otp/verify", handlers.APIVerifyOTP)
			v1.POST("/check", handlers.APICheckNumber)
			v1.GET("/status", handlers.APIStatus)
			v1.GET("/contacts", handlers.APIListContacts)
			v1.POST("/contacts", handlers.APISaveContact)
			v1.GET("/contacts/:number", handlers.APIGetContact)
			v1.PUT("/contacts/:number", handlers.APIUpdateContact)
			v1.DELETE("/contacts/:number", handlers.APIDeleteContact)
			v1.GET("/groups", handlers.APIListGroups)
			v1.POST("/groups/:jid/messages", handlers.APIGroupSendMessage)
			v1.GET("/chats", handlers.APIListChats)
			v1.GET("/chats/:number/messages", handlers.APIChatMessages)
			v1.GET("/media/:cid", handlers.APIServeMedia)
			v1.POST("/broadcasts", handlers.APICreateBroadcast)
			v1.GET("/broadcasts", handlers.APIListBroadcasts)
			v1.GET("/broadcasts/:id", handlers.APIBroadcastStatus)
			v1.GET("/broadcasts/:id/recipients", handlers.APIBroadcastRecipients)
			v1.POST("/broadcasts/:id/cancel", handlers.APICancelBroadcast)
		}
		api.GET("/settings/api-config", handlers.AuthMiddleware(), handlers.GetAPIConfig)
		api.PUT("/settings/api-config", handlers.AuthMiddleware(), handlers.RequireSuperAdmin(), handlers.SaveAPIConfig)
		api.GET("/settings/embedding-models", handlers.AuthMiddleware(), handlers.RequireSuperAdmin(), handlers.ListEmbeddingModels)
		api.GET("/settings/chat-models", handlers.AuthMiddleware(), handlers.RequireSuperAdmin(), handlers.ListChatModels)
		api.GET("/settings/vision-models", handlers.AuthMiddleware(), handlers.RequireSuperAdmin(), handlers.ListVisionModels)

		// Shipping public (search address tidak perlu auth)
		api.GET("/shipping/search-address", handlers.SearchMengantarAddress)
		api.GET("/shipping/addresses", handlers.AuthMiddleware(), handlers.GetMengantarAddresses)
		// Lincah webhook — DIPANGGIL OLEH LINCAH (incoming), publik.
		api.POST("/lincah/webhook", handlers.LincahWebhook)

		auth := api.Group("", handlers.AuthMiddleware())
		auth.Use(handlers.CSRouteGuard()) // CS-only hanya bisa akses agent yang di-assign
		{
			// Endpoint lama (back-compat) -> beroperasi pada agent default (id 1).
			auth.GET("/wa/status", handlers.GetNumberStatus)
			auth.POST("/wa/connect", handlers.ConnectNumber)
			auth.POST("/wa/logout", handlers.LogoutNumber)
			auth.GET("/handoffs", handlers.ListHandoffs)
			auth.DELETE("/handoffs/:sender", handlers.ResumeHandoff)
			auth.GET("/chat-history", handlers.ChatHistory)
			// GET tetap terbuka: dibaca kartu kesiapan di Dashboard (tab yang tetap
			// terlihat manager/CS). Semua mutasi persona & knowledge versi legacy
			// pindah ke group authAI di bawah.
			auth.GET("/settings", handlers.GetSettings)
			auth.GET("/knowledge", handlers.ListKnowledge)

			// Multi-agent (CS).
			auth.GET("/agents", handlers.ListAgents)
			auth.GET("/agents-status", handlers.AgentStatuses)
			// PUT /agents/:id SENGAJA tetap di sini, tidak ikut pindah ke authAkun.
			// Endpoint yang sama dipakai saklar "Balasan AI" dan "Tandai pesan dibaca
			// otomatis" di tab Dashboard — tab yang tetap terlihat untuk manager/CS.
			// Kalau dipindah, manager/CS langsung kena 403 begitu menyalakan AI.
			// Penyaringan field sensitif (persona/tone untuk fitur "ai", nama CS &
			// jadwal untuk fitur "akun") dikerjakan di dalam handler UpdateAgent,
			// bukan di level route ini.
			auth.PUT("/agents/:id", handlers.UpdateAgent)
			auth.GET("/agents/:id/wa/status", handlers.GetNumberStatus)
			auth.POST("/agents/:id/wa/connect", handlers.ConnectNumber)
			auth.POST("/agents/:id/wa/connect-pairing", handlers.ConnectPairingNumber)
			auth.POST("/agents/:id/wa/logout", handlers.LogoutNumber)
			// Catatan pindahan: Learning Engine sekarang di group authAI, dan
			// REST API/webhook per-nomor di group authAkun (lihat di bawah).

			auth.GET("/agents/:id/handoffs", handlers.ListHandoffs)
			// Pipeline & Label — tahap CRM + aturan pelabelan otomatis (non-SaaS)
			auth.GET("/agents/:id/crm/pipeline", handlers.GetPipeline)
			auth.PUT("/agents/:id/crm/pipeline/stages", handlers.SavePipelineStages)
			auth.PUT("/agents/:id/crm/pipeline/config", handlers.SavePipelineConfig)
			auth.POST("/agents/:id/crm/pipeline/rules", handlers.SaveLabelRule)
			auth.PUT("/agents/:id/crm/pipeline/rules", handlers.SaveLabelRule)
			auth.DELETE("/agents/:id/crm/pipeline/rules/:rid", handlers.DeleteLabelRule)
			auth.POST("/agents/:id/crm/pipeline/rules/test", handlers.TestLabelRules)
			// Kontrol AI per kontak (CS dari inbox: jeda/lanjutkan AI, pindah ke CS).
			auth.POST("/agents/:id/contacts/:sender/ai-off", handlers.PauseAIContact)
			auth.POST("/agents/:id/contacts/:sender/ai-on", handlers.ResumeAIContact)
			auth.GET("/agents/:id/contacts/:sender/ai-status", handlers.ContactAIStatus)
			auth.POST("/agents/:id/contacts/:sender/handoff", handlers.ManualHandoffContact)

			// Meta CAPI (label WhatsApp -> event Facebook Ads).
			auth.GET("/agents/:id/meta", handlers.GetMetaConfig)
			auth.PUT("/agents/:id/meta", handlers.SaveMetaConfig)
			auth.POST("/agents/:id/meta/test", handlers.TestMetaEvent)
			auth.GET("/agents/:id/meta/logs", handlers.MetaConversionLogs)
			auth.DELETE("/agents/:id/handoffs/:sender", handlers.ResumeHandoff)
			auth.GET("/agents/:id/chat-history", handlers.ChatHistory)
			// GET knowledge & status crawl tetap di sini: hook-nya dipanggil top-level
			// di Dashboard (polling 4 detik) untuk kartu kesiapan, jadi ikut jalan di
			// semua tab termasuk tab yang masih terlihat manager/CS.
			auth.GET("/agents/:id/settings", handlers.GetSettings)
			auth.GET("/agents/:id/knowledge", handlers.ListKnowledge)
			auth.GET("/agents/:id/crawl", handlers.LatestCrawl)
			auth.GET("/agents/:id/crawl/:jobId", handlers.CrawlStatus)
			auth.GET("/agents/:id/knowledge-usage", handlers.KnowledgeUsage)

			// Fitur jualan: simulator, analitik, inbox.
			auth.POST("/agents/:id/test-chat", handlers.TestChat)
			auth.GET("/agents/:id/analytics", handlers.AgentAnalytics)
			auth.GET("/agents/:id/ai-metrics", handlers.AgentAIMetrics)
			auth.GET("/agents/:id/contacts", handlers.InboxContacts)
			auth.GET("/agents/:id/conversation", handlers.InboxConversation)
			auth.POST("/agents/:id/inbox/:sender/read", handlers.MarkConversationRead)
			auth.DELETE("/agents/:id/conversation", handlers.DeleteInboxConversation)
			auth.GET("/agents/:id/conversation/brief", handlers.GetConversationBrief)
			auth.POST("/agents/:id/conversation/brief", handlers.RefreshConversationBrief)
			auth.POST("/agents/:id/send", handlers.InboxSend)
			auth.POST("/agents/:id/send-media", handlers.InboxSendMedia)
			auth.POST("/agents/:id/messages/:cid/analyze", handlers.ReanalyzeInboxImage)
			auth.POST("/agents/:id/typing", handlers.ChatPresence)
			auth.DELETE("/agents/:id/messages/:msgId", handlers.RevokeMessage)
			auth.GET("/agents/:id/auto-replies", handlers.ListAutoReplies)
			auth.POST("/agents/:id/auto-replies", handlers.CreateAutoReply)
			auth.PUT("/agents/:id/auto-replies/:rid", handlers.UpdateAutoReply)
			auth.DELETE("/agents/:id/auto-replies/:rid", handlers.DeleteAutoReply)
			auth.GET("/agents/:id/flow", handlers.GetFlow)
			auth.POST("/agents/:id/flow", handlers.SaveFlow)
			auth.GET("/agents/:id/templates", handlers.ListTemplates)
			auth.POST("/agents/:id/templates", handlers.CreateTemplate)
			auth.PUT("/agents/:id/templates/:tid", handlers.UpdateTemplate)
			auth.DELETE("/agents/:id/templates/:tid", handlers.DeleteTemplate)
			auth.GET("/agents/:id/templates/:tid/media", handlers.ServeTemplateMedia)
			auth.GET("/agents/:id/crm/contacts", handlers.ListSavedContacts)
			auth.POST("/agents/:id/crm/contacts", handlers.CreateSavedContact)
			auth.PUT("/agents/:id/crm/contacts/:cid", handlers.UpdateSavedContact)
			auth.DELETE("/agents/:id/crm/contacts/:cid", handlers.DeleteSavedContact)
			auth.POST("/agents/:id/crm/contacts/bulk-tag", handlers.BulkTagSavedContacts)
			auth.POST("/agents/:id/crm/contacts/bulk-stage", handlers.BulkStageSavedContacts)
			auth.POST("/agents/:id/crm/contacts/import", handlers.ImportSavedContacts)
			auth.POST("/agents/:id/crm/contacts/bulk-delete", handlers.BulkDeleteSavedContacts)
			auth.GET("/agents/:id/follow-ups", handlers.ListFollowUps)
			auth.POST("/agents/:id/follow-ups", handlers.CreateFollowUp)
			auth.PUT("/agents/:id/follow-ups/:fid", handlers.UpdateFollowUp)
			auth.DELETE("/agents/:id/follow-ups/:fid", handlers.DeleteFollowUp)
			auth.POST("/agents/:id/follow-ups/:fid/enroll", handlers.EnrollFollowUp)

			// Katalog produk.
			auth.GET("/agents/:id/products", handlers.ListProducts)
			auth.POST("/agents/:id/products", handlers.CreateProduct)
			auth.POST("/agents/:id/products/generate-ai", handlers.GenerateProductAIContent)
			auth.PUT("/agents/:id/products/:pid", handlers.UpdateProduct)
			auth.DELETE("/agents/:id/products/:pid", handlers.DeleteProduct)
			auth.POST("/agents/:id/products/:pid/send", handlers.SendProduct)
			auth.GET("/agents/:id/product-orders", handlers.ListProductOrders)
			// Daftar form & kiriman form ikut dirender kartu overview Dashboard -> GET tetap terbuka.
			auth.GET("/agents/:id/ai-forms", handlers.ListAIForms)
			auth.GET("/agents/:id/ai-form-submissions", handlers.ListAIFormSubmissions)
			auth.GET("/agents/:id/broadcast/consent-summary", handlers.BroadcastConsentSummary)
			auth.POST("/agents/:id/broadcast", handlers.CreateBroadcast)
			auth.POST("/agents/:id/broadcast/rotation-test", handlers.TestBroadcastRotation)
			auth.GET("/agents/:id/broadcasts", handlers.ListBroadcasts)
			auth.GET("/agents/:id/broadcasts/:bid", handlers.BroadcastDetail)
			auth.POST("/agents/:id/broadcasts/:bid/cancel", handlers.CancelBroadcast)
			auth.POST("/agents/:id/broadcasts/:bid/resume", handlers.ResumeBroadcast)
			// Blast Multiple Number: data kontak per tenant (impor .xlsx, assign nomor).
			auth.GET("/agents/:id/multi-blast/contacts", handlers.ListMultiBlastContacts)
			auth.POST("/agents/:id/multi-blast/contacts/import", handlers.ImportMultiBlastContacts)
			auth.POST("/agents/:id/multi-blast/contacts/assign", handlers.AssignMultiBlastContacts)
			auth.POST("/agents/:id/multi-blast/contacts/distribute", handlers.DistributeMultiBlastContacts)
			auth.POST("/agents/:id/multi-blast/contacts/delete", handlers.DeleteMultiBlastContacts)
			// Struktur master -> anggota se-tenant; pengecekan super admin ada di handler.
			auth.PUT("/multi-blast/structure", handlers.SaveMultiBlastStructure)
			auth.GET("/agents/:id/chat-contacts", handlers.ChatContacts)
			auth.GET("/agents/:id/wa-contacts", handlers.WAContacts)
			auth.POST("/agents/:id/check-numbers", handlers.CheckNumbersOnWA)
			auth.GET("/agents/:id/groups", handlers.Groups)
			auth.GET("/agents/:id/group-members", handlers.GroupMembers)
			auth.GET("/agents/:id/group-config", handlers.GroupConfig)
			auth.PUT("/agents/:id/group-config", handlers.SaveGroupConfig)
			auth.GET("/agents/:id/group-moderation", handlers.GroupModeration)
			auth.POST("/agents/:id/group-moderation/:logid/confirm-kick", handlers.ConfirmKick)
			auth.POST("/agents/:id/group-moderation/:logid/dismiss", handlers.DismissModeration)
			auth.GET("/agents/:id/labels", handlers.Labels)
			auth.POST("/agents/:id/labels/sync", handlers.SyncLabels)
			auth.GET("/agents/:id/label-contacts", handlers.LabelContacts)
			auth.POST("/agents/:id/schedule", handlers.CreateSchedule)
			auth.GET("/agents/:id/schedules", handlers.ListSchedules)
			auth.DELETE("/agents/:id/schedule/:sid", handlers.CancelSchedule)
			auth.POST("/agents/:id/status", handlers.CreateStatus)
			auth.GET("/agents/:id/statuses", handlers.ListStatuses)
			auth.DELETE("/agents/:id/status/:sid", handlers.CancelStatus)

			// --- Shipping / Ongkir (Mengantar API) ---
			auth.GET("/agents/:id/shipping/estimate", handlers.CheckShipping)
			auth.GET("/agents/:id/shipping/orders", handlers.GetShippingOrders)
			auth.GET("/agents/:id/shipping/orders/:orderId", handlers.GetShippingOrderDetail)
			// Lincah — integrasi pengiriman (gudang, ongkir, resi, lacak).
			auth.GET("/agents/:id/lincah/config", handlers.GetLincahConfig)
			auth.PUT("/agents/:id/lincah/config", handlers.SaveLincahConfig)
			auth.POST("/agents/:id/lincah/test", handlers.TestLincahConnection)
			auth.GET("/agents/:id/lincah/addresses", handlers.LincahListAddresses)
			auth.GET("/agents/:id/lincah/couriers", handlers.LincahListCouriers)
			auth.POST("/agents/:id/lincah/ongkir", handlers.LincahCheckOngkir)
			auth.GET("/agents/:id/lincah/district/search", handlers.LincahSearchDistrictHandler)
			auth.POST("/agents/:id/lincah/orders", handlers.LincahCreateOrder)
			auth.GET("/agents/:id/lincah/orders", handlers.LincahListLocalOrders)
			auth.GET("/agents/:id/lincah/orders/:id", handlers.LincahOrderDetail)
			auth.GET("/agents/:id/lincah/orders/:id/track", handlers.LincahOrderTrack)
			auth.POST("/agents/:id/lincah/orders/:id/cancel", handlers.LincahOrderCancel)
			auth.GET("/agents/:id/lincah/orders/:id/pdf", handlers.LincahOrderPrint)
			auth.GET("/agents/:id/lincah/districts/search", handlers.LincahSearchDistricts)
			auth.POST("/agents/:id/lincah/chat-quote", handlers.LincahChatQuote)
			auth.POST("/agents/:id/shipping/orders", handlers.CreateShippingOrder)
			auth.POST("/agents/:id/shipping/sync-tracking", handlers.SyncShippingTracking)

			// --- Media Assets (untuk AI auto-send media) ---
			auth.GET("/agents/:id/media-assets", handlers.ListMediaAssets)
			auth.POST("/agents/:id/media-assets", handlers.UploadMediaAsset)
			auth.DELETE("/agents/:id/media-assets/:assetId", handlers.DeleteMediaAsset)
			auth.GET("/agents/:id/history-media/:cid", handlers.GetHistoryMedia)
			// --- Team CS Management (admin-only) ---
			auth.GET("/team/users", handlers.RequireTenantAdmin(), handlers.ListTeamUsers)
			auth.POST("/team/users", handlers.RequireTenantAdmin(), handlers.CreateTeamUser)
			auth.PUT("/team/users/:uid", handlers.RequireTenantAdmin(), handlers.UpdateTeamUser)
			auth.DELETE("/team/users/:uid", handlers.RequireTenantAdmin(), handlers.DeleteTeamUser)
			auth.GET("/team/activity", handlers.RequireTenantAdmin(), handlers.ListCSActivity)

			auth.GET("/agents/:id/history-sync/status", handlers.GetHistorySyncStatus)
			auth.POST("/agents/:id/history-sync", handlers.RequestHistorySync)
			auth.POST("/agents/:id/history-sync/resync", handlers.RequestHistoryResync)
			auth.GET("/agents/:id/inbox/events", handlers.InboxEvents)
			auth.POST("/agents/:id/inbox/reset", handlers.RequireTenantAdmin(), handlers.ResetAgentInbox)
			auth.GET("/agents/:id/inbox/unread-summary", handlers.InboxUnreadSummary)
			auth.GET("/agents/:id/link-preview", handlers.LinkPreview)
			auth.POST("/agents/:id/inbox/client-debug", handlers.InboxClientDebug)
			auth.GET("/agents/:id/inbox/client-debug", handlers.InboxClientDebugDump)
		}

		// authAI = super admin + user yang di-grant fitur "ai".
		// Isinya semua yang mengubah otak AI: persona/system prompt, knowledge,
		// crawl website, dan seluruh tab AI Learning. Sengaja hanya mutasi yang
		// masuk sini; GET yang ikut dipakai kartu kesiapan Dashboard tetap di
		// group `auth` supaya tab Dashboard tidak error 403 saat dibuka manager/CS.
		authAI := api.Group("", handlers.AuthMiddleware(), handlers.RequireFeature(models.FeatureAI))
		{
			// Persona / system prompt (versi legacy tanpa :id dan versi per-agent).
			// Catatan: dua route ini nol pemanggil di frontend saat ini — persona
			// sebenarnya disimpan lewat PUT /agents/:id. Dikunci untuk jaga-jaga.
			authAI.PUT("/settings", handlers.UpdateSettings)
			authAI.PUT("/agents/:id/settings", handlers.UpdateSettings)

			// Knowledge versi legacy (tanpa :id).
			authAI.POST("/knowledge", handlers.CreateKnowledge)
			authAI.POST("/knowledge/generate", handlers.GenerateKnowledge)
			authAI.POST("/knowledge/import", handlers.ImportKnowledge)
			authAI.PUT("/knowledge/:kid", handlers.UpdateKnowledge)
			authAI.DELETE("/knowledge/:kid", handlers.DeleteKnowledge)

			// Knowledge per-agent + wizard setup cepat.
			authAI.POST("/agents/:id/setup-wizard", handlers.SetupWizard)
			authAI.POST("/agents/:id/knowledge", handlers.CreateKnowledge)
			authAI.POST("/agents/:id/knowledge/generate", handlers.GenerateKnowledge)
			authAI.POST("/agents/:id/knowledge/import", handlers.ImportKnowledge)
			authAI.PUT("/agents/:id/knowledge/:kid", handlers.UpdateKnowledge)
			authAI.DELETE("/agents/:id/knowledge-all", handlers.DeleteAllKnowledge)
			authAI.DELETE("/agents/:id/knowledge/:kid", handlers.DeleteKnowledge)

			// Latih AI dari website: crawl (background) -> pilih halaman -> embed jadi knowledge.
			// GET status crawl-nya tetap di group `auth` (dipolling Dashboard).
			authAI.POST("/agents/:id/crawl", handlers.StartCrawl)
			authAI.POST("/agents/:id/crawl/:jobId/train", handlers.TrainCrawlPages)
			authAI.POST("/agents/:id/crawl/:jobId/train/stop", handlers.StopTraining)
			authAI.POST("/agents/:id/persona/regenerate", handlers.RegeneratePersona)

			// Form layanan (AI Form) — hanya mutasinya, daftar & kirimannya dibaca Dashboard.
			authAI.POST("/agents/:id/ai-forms", handlers.CreateAIForm)
			authAI.PUT("/agents/:id/ai-forms/:fid", handlers.UpdateAIForm)
			authAI.DELETE("/agents/:id/ai-forms/:fid", handlers.DeleteAIForm)

			// --- AI Learning (AI belajar dari chat CS manusia) ---
			// Tab AI Learning berdiri sendiri dan tidak dipakai tab lain, jadi GET-nya
			// ikut dikunci penuh.
			authAI.POST("/agents/:id/learning/run", handlers.StartLearning)
			authAI.POST("/agents/:id/learning/clone-profile-to-all", handlers.CloneLearningProfileToAll)
			authAI.POST("/agents/:id/learning/enable-all", handlers.EnableLearningForAll)
			authAI.GET("/agents/:id/learning/status", handlers.GetLearningStatus)
			authAI.GET("/agents/:id/learning/score", handlers.GetLearningScore)
			authAI.GET("/agents/:id/learning/runs", handlers.GetLearningRuns)
			authAI.GET("/agents/:id/learning/runs/:rid", handlers.GetLearningRun)
			authAI.GET("/agents/:id/learning/patterns", handlers.GetLearningPatterns)
			authAI.POST("/agents/:id/learning/patterns/:pid/apply", handlers.ApplyLearningPattern)
			authAI.POST("/agents/:id/learning/patterns/:pid/reject", handlers.RejectLearningPattern)
			authAI.POST("/agents/:id/learning/patterns/apply-all", handlers.ApplyAllPatterns)
			authAI.GET("/agents/:id/learning/snapshots", handlers.GetSnapshots)
			authAI.POST("/agents/:id/learning/snapshots", handlers.CreateSnapshotAPI)
			authAI.POST("/agents/:id/learning/snapshots/:sid/rollback", handlers.RollbackSnapshot)
			authAI.GET("/agents/:id/learning/config", handlers.GetLearningConfigAPI)
			authAI.PUT("/agents/:id/learning/config", handlers.SaveLearningConfigAPI)
		}

		// authAkun = super admin + user yang di-grant fitur "akun".
		// Isinya section Akun: REST API/webhook per-nomor, plus tambah & hapus CS.
		// Tab Widget tidak punya endpoint sendiri (semuanya dibangun di sisi klien),
		// jadi tidak ada yang perlu dipindah untuk tab itu.
		// GET /agents & GET /agents-status WAJIB tetap di group `auth` — dipakai
		// switcher CS di sidebar dan sumber data tab Dashboard/Pengaturan.
		authAkun := api.Group("", handlers.AuthMiddleware(), handlers.RequireFeature(models.FeatureAkun))
		{
			// BISA DIBALIK: kalau nanti manager/CS boleh menambah & menghapus nomor CS,
			// cukup pindahkan dua baris ini kembali ke group `auth` (tombolnya di
			// sidebar & dialog "Kelola CS" — ikut disembunyikan di frontend).
			authAkun.POST("/agents", handlers.CreateAgent)
			authAkun.DELETE("/agents/:id", handlers.DeleteAgent)

			// REST API & webhook per-nomor (kelola key/URL dari dashboard).
			// Eksklusif tab "REST API" -> dikunci penuh termasuk GET-nya.
			authAkun.GET("/agents/:id/api", handlers.GetAPISettings)
			authAkun.POST("/agents/:id/api/key", handlers.RotateAPIKey)
			authAkun.DELETE("/agents/:id/api/key", handlers.RevokeAPIKey)
			authAkun.PUT("/agents/:id/api/webhook", handlers.SaveWebhook)
			authAkun.POST("/agents/:id/api/webhook-secret", handlers.RotateWebhookSecret)
			authAkun.POST("/agents/:id/api/webhook/test", handlers.TestWebhook)
			authAkun.POST("/agents/:id/api/test-message", handlers.TestAPIMessage)
		}

		// admin = super admin saja (User.IsSuperAdmin), tanpa bisa di-grant.
		// Manajemen akun tim: bikin/ubah/hapus user manager & CS beserta fiturnya.
		admin := api.Group("", handlers.AuthMiddleware(), handlers.RequireSuperAdmin())
		{
			admin.GET("/users", handlers.ListUsers)
			admin.POST("/users", handlers.CreateUser)
			admin.PUT("/users/:uid", handlers.UpdateUser)
			admin.POST("/users/:uid/password", handlers.ResetUserPassword)
			admin.DELETE("/users/:uid", handlers.DeleteUser)
		}
	}

	port := config.Env("PORT", "3030")
	srv := &http.Server{Addr: ":" + port, Handler: r}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()
	ui.StartupOK(port)

	<-appCtx.Done()
	log.Println("Mematikan server (graceful)…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown gagal: %v", err)
	}
	log.Println("Server berhenti.")
}
