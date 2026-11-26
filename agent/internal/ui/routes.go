package ui

import "net/http"

// route is one entry in the UI route inventory. uiRoutes is the single source
// for the mux, the route tests, and the later BIT-020 browser check.
type route struct {
	Name     string
	Method   string
	Pattern  string
	Template string
	// Auth is true when the route is behind the administrator session. The
	// setup, login, and static routes are the deliberate exceptions.
	Auth bool
	// Responsive is true when BIT-020 must load the route at both phone and
	// desktop widths. Redirects, actions, fragments, and static files are not
	// full pages.
	Responsive bool
	// Status is the response status the route's route-test request returns.
	Status  int
	Handler func(*handler) http.Handler
}

func (r route) registration() string {
	if r.Method == "" {
		return r.Pattern
	}
	return r.Method + " " + r.Pattern
}

func routeHandler(fn func(*handler, http.ResponseWriter, *http.Request)) func(*handler) http.Handler {
	return func(h *handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fn(h, w, r)
		})
	}
}

var uiRoutes = []route{
	{
		Name: "root", Method: http.MethodGet, Pattern: "/ui", Auth: true, Status: http.StatusSeeOther,
		Handler: routeHandler(func(_ *handler, w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, BasePath+"/", http.StatusSeeOther)
		}),
	},
	{
		Name: "root-slash", Method: http.MethodGet, Pattern: "/ui/{$}", Auth: true, Status: http.StatusSeeOther,
		Handler: routeHandler(func(_ *handler, w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, BasePath+"/wizard", http.StatusSeeOther)
		}),
	},
	{
		Name: "login", Method: http.MethodGet, Pattern: "/ui/login", Template: "login",
		Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showLogin),
	},
	{
		Name: "login-submit", Method: http.MethodPost, Pattern: "/ui/login", Template: "login",
		Status: http.StatusUnauthorized, Handler: routeHandler((*handler).login),
	},
	{
		Name: "setup", Method: http.MethodGet, Pattern: "/ui/setup", Template: "setup",
		Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showSetup),
	},
	{
		Name: "setup-submit", Method: http.MethodPost, Pattern: "/ui/setup", Template: "setup",
		Status: http.StatusUnauthorized, Handler: routeHandler((*handler).setup),
	},
	{
		Name: "logout", Method: http.MethodPost, Pattern: "/ui/logout", Auth: true,
		Status: http.StatusSeeOther, Handler: routeHandler((*handler).logout),
	},
	{
		Name: "wizard", Method: http.MethodGet, Pattern: "/ui/wizard", Template: "welcome",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).welcome),
	},
	{
		Name: "wizard-network", Method: http.MethodGet, Pattern: "/ui/wizard/network", Template: "network",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showNetwork),
	},
	{
		Name: "wizard-network-submit", Method: http.MethodPost, Pattern: "/ui/wizard/network",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).enterNetwork),
	},
	{
		Name: "wizard-storage", Method: http.MethodGet, Pattern: "/ui/wizard/storage", Template: "storage",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showStorage),
	},
	{
		Name: "wizard-storage-submit", Method: http.MethodPost, Pattern: "/ui/wizard/storage",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).enterStorage),
	},
	{
		Name: "wizard-camera", Method: http.MethodGet, Pattern: "/ui/wizard/camera", Template: "camera",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showCamera),
	},
	{
		Name: "wizard-camera-continue", Method: http.MethodPost, Pattern: "/ui/wizard/camera/continue",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).enterCamera),
	},
	{
		Name: "wizard-camera-discover", Method: http.MethodPost, Pattern: "/ui/wizard/camera/discover",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).refreshDiscovery),
	},
	{
		Name: "wizard-camera-submit", Method: http.MethodPost, Pattern: "/ui/wizard/camera", Template: "camera",
		Auth: true, Status: http.StatusUnprocessableEntity, Handler: routeHandler((*handler).submitCamera),
	},
	{
		Name: "wizard-camera-settings", Method: http.MethodGet, Pattern: "/ui/wizard/camera/{ref}/settings",
		Template: "camera", Auth: true, Responsive: true, Status: http.StatusOK,
		Handler: routeHandler((*handler).showCameraSettings),
	},
	{
		Name: "wizard-camera-settings-submit", Method: http.MethodPost, Pattern: "/ui/wizard/camera/{ref}/settings",
		Template: "camera", Auth: true, Status: http.StatusUnprocessableEntity,
		Handler: routeHandler((*handler).submitCameraSettings),
	},
	{
		Name: "wizard-camera-remove", Method: http.MethodPost, Pattern: "/ui/wizard/camera/{ref}/remove",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).removePendingCamera),
	},
	{
		Name: "wizard-review", Method: http.MethodGet, Pattern: "/ui/wizard/review", Template: "review",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showReview),
	},
	{
		Name: "wizard-apply", Method: http.MethodPost, Pattern: "/ui/wizard/apply",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).apply),
	},
	{
		Name: "wizard-operation", Method: http.MethodGet, Pattern: "/ui/wizard/apply", Template: "operation",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showApplyOperation),
	},
	{
		Name: "wizard-operation-fragment", Method: http.MethodGet, Pattern: "/ui/wizard/apply/fragment",
		Template: "operation-panel", Auth: true, Status: http.StatusOK,
		Handler: routeHandler((*handler).showApplyOperationFragment),
	},
	{
		Name: "wizard-result", Method: http.MethodGet, Pattern: "/ui/wizard/result", Template: "operation-result",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showApplyResult),
	},
	{
		Name: "wizard-done", Method: http.MethodGet, Pattern: "/ui/wizard/done", Template: "done",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showDone),
	},
	{
		Name: "wizard-cancel", Method: http.MethodPost, Pattern: "/ui/wizard/cancel",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).cancel),
	},
	{
		Name: "cameras", Method: http.MethodGet, Pattern: "/ui/cameras", Template: "cameras",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showCameras),
	},
	{
		Name: "camera-edit", Method: http.MethodGet, Pattern: "/ui/cameras/{id}/edit", Template: "camera",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showCameraEdit),
	},
	{
		Name: "camera-edit-submit", Method: http.MethodPost, Pattern: "/ui/cameras/{id}/edit", Template: "camera",
		Auth: true, Status: http.StatusUnprocessableEntity, Handler: routeHandler((*handler).submitCameraEdit),
	},
	{
		Name: "camera-edit-discover", Method: http.MethodPost, Pattern: "/ui/cameras/{id}/discover",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).refreshCameraEditDiscovery),
	},
	{
		Name: "camera-snapshot", Method: http.MethodPost, Pattern: "/ui/cameras/{id}/snapshot",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).testCameraSnapshot),
	},
	{
		Name: "camera-picture", Method: http.MethodGet, Pattern: "/ui/cameras/{id}/picture",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showCameraPicture),
	},
	{
		Name: "camera-remove", Method: http.MethodGet, Pattern: "/ui/cameras/{id}/remove", Template: "camera-remove",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showCameraRemove),
	},
	{
		Name: "camera-remove-submit", Method: http.MethodPost, Pattern: "/ui/cameras/{id}/remove", Template: "camera-remove",
		Auth: true, Status: http.StatusOK, Handler: routeHandler((*handler).removeCamera),
	},
	{
		Name: "status", Method: http.MethodGet, Pattern: "/ui/status", Template: "status",
		Auth: true, Responsive: true, Status: http.StatusOK, Handler: routeHandler((*handler).showStatus),
	},
	{
		Name: "updates-check", Method: http.MethodPost, Pattern: "/ui/status/updates/check",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).checkForUpdate),
	},
	{
		Name: "updates-install", Method: http.MethodPost, Pattern: "/ui/status/updates/install",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).startUpdateInstall),
	},
	{
		Name: "update-rollback", Method: http.MethodGet, Pattern: "/ui/status/updates/rollback",
		Template: "update-rollback", Auth: true, Responsive: true, Status: http.StatusOK,
		Handler: routeHandler((*handler).showUpdateRollback),
	},
	{
		Name: "update-rollback-submit", Method: http.MethodPost, Pattern: "/ui/status/updates/rollback",
		Auth: true, Status: http.StatusSeeOther, Handler: routeHandler((*handler).startUpdateRollback),
	},
	{
		Name: "status-fragment", Method: http.MethodGet, Pattern: "/ui/status/fragment", Template: "status-panel",
		Auth: true, Status: http.StatusOK, Handler: routeHandler((*handler).showStatusFragment),
	},
	{
		Name: "status-updates-fragment", Method: http.MethodGet, Pattern: "/ui/status/updates/fragment", Template: "update-card-fragment",
		Auth: true, Status: http.StatusOK, Handler: routeHandler((*handler).showUpdateFragment),
	},
	{
		Name: "static", Method: http.MethodGet, Pattern: "/ui/static/", Status: http.StatusOK,
		Handler: func(h *handler) http.Handler { return h.staticFiles() },
	},
	{
		Name: "not-found", Pattern: "/ui/", Template: "error",
		Auth: true, Responsive: true, Status: http.StatusNotFound, Handler: routeHandler((*handler).notFound),
	},
}

// routeMux records every pattern it receives so a test can compare the mux
// against uiRoutes.
type routeMux struct {
	*http.ServeMux
	registered []string
}

func newRouteMux() *routeMux {
	return &routeMux{ServeMux: http.NewServeMux()}
}

func (m *routeMux) Handle(pattern string, handler http.Handler) {
	m.registered = append(m.registered, pattern)
	m.ServeMux.Handle(pattern, handler)
}

func (m *routeMux) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	m.Handle(pattern, http.HandlerFunc(handler))
}

func (h *handler) registerRoutes(mux *routeMux) {
	for _, route := range uiRoutes {
		mux.Handle(route.registration(), route.Handler(h))
	}
}
