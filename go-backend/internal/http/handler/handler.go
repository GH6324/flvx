package handler

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/middleware"
	"go-backend/internal/http/response"
	"go-backend/internal/security"
	"go-backend/internal/store/sqlite"
	"go-backend/internal/ws"
)

type Handler struct {
	repo      *sqlite.Repository
	jwtSecret string
	wsServer  *ws.Server
}

type loginRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	CaptchaID string `json:"captchaId"`
}

type nameRequest struct {
	Name string `json:"name"`
}

type configSingleRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type changePasswordRequest struct {
	NewUsername     string `json:"newUsername"`
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	ConfirmPassword string `json:"confirmPassword"`
}

type flowItem struct {
	N string `json:"n"`
	U int64  `json:"u"`
	D int64  `json:"d"`
}

func New(repo *sqlite.Repository, jwtSecret string) *Handler {
	return &Handler{repo: repo, jwtSecret: jwtSecret, wsServer: ws.NewServer(repo, jwtSecret)}
}

func (h *Handler) WebSocketHandler() http.Handler {
	return h.wsServer
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/user/login", h.login)
	mux.HandleFunc("/api/v1/user/list", h.userList)
	mux.HandleFunc("/api/v1/config/get", h.getConfigByName)
	mux.HandleFunc("/api/v1/config/list", h.getConfigs)
	mux.HandleFunc("/api/v1/config/update", h.updateConfigs)
	mux.HandleFunc("/api/v1/config/update-single", h.updateSingleConfig)
	mux.HandleFunc("/api/v1/captcha/check", h.checkCaptcha)
	mux.HandleFunc("/api/v1/user/package", h.userPackage)
	mux.HandleFunc("/api/v1/user/updatePassword", h.updatePassword)
	mux.HandleFunc("/api/v1/node/list", h.nodeList)
	mux.HandleFunc("/api/v1/tunnel/list", h.tunnelList)
	mux.HandleFunc("/api/v1/forward/list", h.forwardList)
	mux.HandleFunc("/api/v1/speed-limit/list", h.speedLimitList)
	mux.HandleFunc("/api/v1/tunnel/user/tunnel", h.userTunnelVisibleList)
	mux.HandleFunc("/api/v1/tunnel/user/list", h.userTunnelList)
	mux.HandleFunc("/api/v1/group/tunnel/list", h.tunnelGroupList)
	mux.HandleFunc("/api/v1/group/user/list", h.userGroupList)
	mux.HandleFunc("/api/v1/group/permission/list", h.groupPermissionList)

	mux.HandleFunc("/flow/test", h.flowTest)
	mux.HandleFunc("/flow/config", h.flowConfig)
	mux.HandleFunc("/flow/upload", h.flowUpload)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	var req loginRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.Err(500, "请求参数错误"))
		return
	}

	if strings.TrimSpace(req.Username) == "" {
		response.WriteJSON(w, response.Err(500, "用户名不能为空"))
		return
	}
	if strings.TrimSpace(req.Password) == "" {
		response.WriteJSON(w, response.Err(500, "密码不能为空"))
		return
	}

	captchaEnabled, err := h.captchaEnabled()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if captchaEnabled && strings.TrimSpace(req.CaptchaID) == "" {
		response.WriteJSON(w, response.ErrDefault("验证码校验失败"))
		return
	}

	user, err := h.repo.GetUserByUsername(req.Username)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if user == nil {
		response.WriteJSON(w, response.ErrDefault("账号或密码错误"))
		return
	}
	if user.Pwd != security.MD5(req.Password) {
		response.WriteJSON(w, response.ErrDefault("账号或密码错误"))
		return
	}
	if user.Status == 0 {
		response.WriteJSON(w, response.ErrDefault("账号被停用"))
		return
	}

	token, err := auth.GenerateToken(user.ID, user.User, user.RoleID, h.jwtSecret)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	requirePasswordChange := req.Username == "admin_user" || req.Password == "admin_user"
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"token":                 token,
		"name":                  user.User,
		"role_id":               user.RoleID,
		"requirePasswordChange": requirePasswordChange,
	}))
}

func (h *Handler) getConfigByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	var req nameRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("配置名称不能为空"))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		response.WriteJSON(w, response.ErrDefault("配置名称不能为空"))
		return
	}

	cfg, err := h.repo.GetConfigByName(req.Name)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if cfg == nil {
		response.WriteJSON(w, response.ErrDefault("配置不存在"))
		return
	}

	response.WriteJSON(w, response.OK(cfg))
}

func (h *Handler) getConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	cfgMap, err := h.repo.ListConfigs()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(cfgMap))
}

func (h *Handler) userList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	users, err := h.repo.ListUsers()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(users))
}

func (h *Handler) nodeList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	items, err := h.repo.ListNodes()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) tunnelList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	items, err := h.repo.ListTunnels()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) forwardList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	items, err := h.repo.ListForwards()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) speedLimitList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	items, err := h.repo.ListSpeedLimits()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) userTunnelVisibleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	userID, err := userIDFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}

	items, err := h.repo.ListUserAccessibleTunnels(userID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) userTunnelList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	var req struct {
		UserID int64 `json:"userId"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	if req.UserID <= 0 {
		response.WriteJSON(w, response.OK([]interface{}{}))
		return
	}

	tunnels, err := h.repo.GetUserPackageTunnels(req.UserID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	out := make([]map[string]interface{}, 0, len(tunnels))
	for _, t := range tunnels {
		item := map[string]interface{}{
			"id":             t.ID,
			"userId":         t.UserID,
			"tunnelId":       t.TunnelID,
			"tunnelName":     t.TunnelName,
			"status":         1,
			"flow":           t.Flow,
			"num":            t.Num,
			"expTime":        t.ExpTime,
			"flowResetTime":  t.FlowResetTime,
			"inFlow":         t.InFlow,
			"outFlow":        t.OutFlow,
			"tunnelFlow":     t.TunnelFlow,
			"speedId":        nil,
			"speedLimitName": nil,
		}
		if t.SpeedID.Valid {
			item["speedId"] = t.SpeedID.Int64
		}
		if t.SpeedLimit.Valid {
			item["speedLimitName"] = t.SpeedLimit.String
		}
		out = append(out, item)
	}
	response.WriteJSON(w, response.OK(out))
}

func (h *Handler) tunnelGroupList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	items, err := h.repo.ListTunnelGroups()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) userGroupList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	items, err := h.repo.ListUserGroups()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) groupPermissionList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	items, err := h.repo.ListGroupPermissions()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OK(items))
}

func (h *Handler) checkCaptcha(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	enabled, err := h.captchaEnabled()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if enabled {
		response.WriteJSON(w, response.OK(1))
		return
	}
	response.WriteJSON(w, response.OK(0))
}

func (h *Handler) flowTest(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("test"))
}

func (h *Handler) flowConfig(w http.ResponseWriter, r *http.Request) {
	secret := r.URL.Query().Get("secret")
	if ok, _ := h.repo.NodeExistsBySecret(secret); !ok {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
		return
	}

	_, _ = readAndDecryptFlowBody(r.Body, secret)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) flowUpload(w http.ResponseWriter, r *http.Request) {
	secret := r.URL.Query().Get("secret")
	if ok, _ := h.repo.NodeExistsBySecret(secret); !ok {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok"))
		return
	}

	raw, err := readAndDecryptFlowBody(r.Body, secret)
	if err == nil && strings.TrimSpace(raw) != "" {
		var items []flowItem
		if json.Unmarshal([]byte(raw), &items) == nil {
			for _, item := range items {
				parts := strings.Split(item.N, "_")
				if len(parts) < 3 || item.N == "web_api" {
					continue
				}
				forwardID, err1 := strconv.ParseInt(parts[0], 10, 64)
				userID, err2 := strconv.ParseInt(parts[1], 10, 64)
				userTunnelID, err3 := strconv.ParseInt(parts[2], 10, 64)
				if err1 != nil || err2 != nil || err3 != nil {
					continue
				}
				_ = h.repo.AddFlow(forwardID, userID, userTunnelID, item.D, item.U)
			}
		}
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (h *Handler) updateConfigs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	var payload map[string]string
	if err := decodeJSON(r.Body, &payload); err != nil {
		response.WriteJSON(w, response.ErrDefault("配置数据不能为空"))
		return
	}
	if len(payload) == 0 {
		response.WriteJSON(w, response.ErrDefault("配置数据不能为空"))
		return
	}

	now := time.Now().UnixMilli()
	for k, v := range payload {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		if err := h.repo.UpsertConfig(key, v, now); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) updateSingleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	var req configSingleRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("配置名称不能为空"))
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		response.WriteJSON(w, response.ErrDefault("配置名称不能为空"))
		return
	}
	if strings.TrimSpace(req.Value) == "" {
		response.WriteJSON(w, response.ErrDefault("配置值不能为空"))
		return
	}

	if err := h.repo.UpsertConfig(strings.TrimSpace(req.Name), req.Value, time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) userPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	claims, ok := r.Context().Value(middleware.ClaimsContextKey).(auth.Claims)
	if !ok {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}

	userID, err := parseUserID(claims.Sub)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}

	user, err := h.repo.GetUserByID(userID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if user == nil {
		response.WriteJSON(w, response.ErrDefault("用户不存在"))
		return
	}

	tunnels, err := h.repo.GetUserPackageTunnels(userID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	forwards, err := h.repo.GetUserPackageForwards(userID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	stats, err := h.repo.GetStatisticsFlows(userID, 24)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	sort.Slice(stats, func(i, j int) bool { return stats[i].ID < stats[j].ID })

	tunnelOut := make([]map[string]interface{}, 0, len(tunnels))
	for _, t := range tunnels {
		item := map[string]interface{}{
			"id":             t.ID,
			"userId":         t.UserID,
			"tunnelId":       t.TunnelID,
			"tunnelName":     t.TunnelName,
			"tunnelFlow":     t.TunnelFlow,
			"flow":           t.Flow,
			"inFlow":         t.InFlow,
			"outFlow":        t.OutFlow,
			"num":            t.Num,
			"flowResetTime":  t.FlowResetTime,
			"expTime":        t.ExpTime,
			"speedId":        nil,
			"speedLimitName": nil,
			"speed":          nil,
		}
		if t.SpeedID.Valid {
			item["speedId"] = t.SpeedID.Int64
		}
		if t.SpeedLimit.Valid {
			item["speedLimitName"] = t.SpeedLimit.String
		}
		if t.Speed.Valid {
			item["speed"] = t.Speed.Int64
		}
		tunnelOut = append(tunnelOut, item)
	}

	forwardOut := make([]map[string]interface{}, 0, len(forwards))
	for _, f := range forwards {
		item := map[string]interface{}{
			"id":          f.ID,
			"name":        f.Name,
			"tunnelId":    f.TunnelID,
			"tunnelName":  f.TunnelName,
			"inIp":        f.InIP,
			"inPort":      nil,
			"remoteAddr":  f.RemoteAddr,
			"inFlow":      f.InFlow,
			"outFlow":     f.OutFlow,
			"status":      f.Status,
			"createdTime": f.CreatedAt,
		}
		if f.InPort.Valid {
			item["inPort"] = f.InPort.Int64
		}
		forwardOut = append(forwardOut, item)
	}

	payload := map[string]interface{}{
		"userInfo": map[string]interface{}{
			"id":            user.ID,
			"name":          user.User,
			"user":          user.User,
			"status":        user.Status,
			"flow":          user.Flow,
			"inFlow":        user.InFlow,
			"outFlow":       user.OutFlow,
			"num":           user.Num,
			"expTime":       user.ExpTime,
			"flowResetTime": user.FlowResetTime,
			"createdTime":   user.CreatedTime,
			"updatedTime":   nullableNullInt64(user.UpdatedTime),
		},
		"tunnelPermissions": tunnelOut,
		"forwards":          forwardOut,
		"statisticsFlows":   stats,
	}

	response.WriteJSON(w, response.OK(payload))
}

func (h *Handler) updatePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}

	claims, ok := r.Context().Value(middleware.ClaimsContextKey).(auth.Claims)
	if !ok {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}

	userID, err := parseUserID(claims.Sub)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return
	}

	var req changePasswordRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("修改账号密码时发生错误"))
		return
	}

	if strings.TrimSpace(req.NewUsername) == "" {
		response.WriteJSON(w, response.ErrDefault("新用户名不能为空"))
		return
	}
	if strings.TrimSpace(req.CurrentPassword) == "" {
		response.WriteJSON(w, response.ErrDefault("当前密码不能为空"))
		return
	}
	if strings.TrimSpace(req.NewPassword) == "" {
		response.WriteJSON(w, response.ErrDefault("新密码不能为空"))
		return
	}
	if strings.TrimSpace(req.ConfirmPassword) == "" {
		response.WriteJSON(w, response.ErrDefault("确认密码不能为空"))
		return
	}
	if req.NewPassword != req.ConfirmPassword {
		response.WriteJSON(w, response.ErrDefault("新密码和确认密码不匹配"))
		return
	}

	user, err := h.repo.GetUserByID(userID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if user == nil {
		response.WriteJSON(w, response.ErrDefault("用户不存在"))
		return
	}

	if user.Pwd != security.MD5(req.CurrentPassword) {
		response.WriteJSON(w, response.ErrDefault("当前密码错误"))
		return
	}

	exists, err := h.repo.UsernameExistsExceptID(req.NewUsername, userID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if exists {
		response.WriteJSON(w, response.ErrDefault("用户名已存在"))
		return
	}

	if err := h.repo.UpdateUserNameAndPassword(userID, req.NewUsername, security.MD5(req.NewPassword), time.Now().UnixMilli()); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}

	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) captchaEnabled() (bool, error) {
	cfg, err := h.repo.GetConfigByName("captcha_enabled")
	if err != nil {
		return false, err
	}
	if cfg == nil {
		return false, nil
	}
	return strings.EqualFold(cfg.Value, "true"), nil
}

func decodeJSON(body io.ReadCloser, out interface{}) error {
	defer body.Close()
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

func parseUserID(sub string) (int64, error) {
	id, err := strconv.ParseInt(sub, 10, 64)
	if err != nil || id <= 0 {
		return 0, strconv.ErrSyntax
	}
	return id, nil
}

func userIDFromRequest(r *http.Request) (int64, error) {
	claims, ok := r.Context().Value(middleware.ClaimsContextKey).(auth.Claims)
	if !ok {
		return 0, strconv.ErrSyntax
	}
	return parseUserID(claims.Sub)
}

func nullableNullInt64(v sql.NullInt64) interface{} {
	if v.Valid {
		return v.Int64
	}
	return nil
}

func readAndDecryptFlowBody(body io.ReadCloser, secret string) (string, error) {
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "", nil
	}

	var wrap struct {
		Encrypted bool   `json:"encrypted"`
		Data      string `json:"data"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil || !wrap.Encrypted || strings.TrimSpace(wrap.Data) == "" {
		return text, nil
	}

	crypto, err := security.NewAESCrypto(secret)
	if err != nil {
		return text, nil
	}
	plain, err := crypto.Decrypt(wrap.Data)
	if err != nil {
		return text, nil
	}
	return string(plain), nil
}
