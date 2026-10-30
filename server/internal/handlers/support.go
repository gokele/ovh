package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/types"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// 来自 EU/CA/US 官方 /1.0/support.json,三站点的分类枚举一致。
var supportCategories = []string{"assistance", "billing", "incident"}
var supportProducts = []string{"dedicated", "vps", "dedicated-billing", "dedicated-other", "publiccloud", "network", "domain", "hosting", "storage", "dedicatedcloud", "iaas", "cdn", "exchange", "mail", "ssl", "vac", "housing", "adsl", "fax", "sms", "voip", "telecom-billing", "telecom-other", "web-billing", "web-other"}
var supportSubcategories = []string{"alerts", "autorenew", "bill", "down", "inProgress", "new", "other", "perfs", "start", "usage"}
var errSupportDetailID = errors.New("工单详情缺少或返回错误的内部 ID")

// supportMember 验证官方枚举,不向 OVH 透传任意分类字符串。
func supportMember(value string, choices []string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

// supportClient 固定实际账户,不可用时返回带翻译码的响应;每个工作流独享 SDK client。
func supportClient(state *app.State, c *gin.Context) (types.OVHAccount, *ovhsdk.Client, error) {
	acc, ok := state.FindAccount(c.Query("account"))
	if !ok {
		c.JSON(400, gin.H{"success": false, "error": "OVH 账户不存在", "code": "SUPPORT_ACCOUNT_NOT_FOUND"})
		return acc, nil, fmt.Errorf("OVH 账户不存在")
	}
	client, err := state.OVH.IsolatedClientFor(acc.ID)
	if err == nil {
		client.Timeout = 15 * time.Second
	} else {
		c.JSON(400, gin.H{"success": false, "error": "OVH 账户无法使用,请检查账户凭据和区域设置", "code": "SUPPORT_ACCOUNT_UNAVAILABLE"})
	}
	return acc, client, err
}

// supportErrorResponse 同时提供默认中文、翻译码和上游原文,列表部分失败也复用。
func supportErrorResponse(err error, mutation bool) (int, gin.H) {
	code := http.StatusBadGateway
	var apiErr *ovhsdk.APIError
	if errors.As(err, &apiErr) && apiErr.Code >= 400 && apiErr.Code < 500 {
		code = apiErr.Code
	}
	message := ovh.Explain(err)
	payload := gin.H{"success": false, "code": "SUPPORT_REQUEST_FAILED", "params": gin.H{"detail": err.Error()}}
	if errors.Is(err, errSupportDetailID) {
		payload["code"] = "SUPPORT_DETAIL_INVALID"
		delete(payload, "params")
	}
	if mutation && (code >= 500 || code == 408 || code == 429) {
		message = "提交结果未能确认,请刷新工单或到 OVH 官网核对,避免重复发送。" + message
		payload["code"] = "SUPPORT_SUBMISSION_UNCONFIRMED"
	}
	payload["error"] = message
	return code, payload
}

// supportFailure 保留上游状态,业务 401 不触发前端应用退出登录。
func supportFailure(c *gin.Context, err error, mutation bool) {
	status, payload := supportErrorResponse(err, mutation)
	c.JSON(status, payload)
}

// GetSupportOptions 返回当前账户和官方类别,不创建工单。
func GetSupportOptions(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		acc, _, err := supportClient(state, c)
		if err != nil {
			return
		}
		c.JSON(200, gin.H{"accountId": acc.ID, "accountName": acc.Name, "categories": supportCategories, "products": supportProducts, "subcategories": supportSubcategories})
	}
}

// GetSupportServices 读取所选产品的服务名;其他产品仍可手动填写 serviceName。
func GetSupportServices(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, client, err := supportClient(state, c)
		if err != nil {
			return
		}
		product := c.Query("product")
		if !supportMember(product, supportProducts) {
			c.JSON(400, gin.H{"success": false, "error": "请选择有效产品", "code": "SUPPORT_PRODUCT_INVALID"})
			return
		}
		names := []string{}
		switch product {
		case "dedicated", "dedicated-billing", "dedicated-other":
			err = client.GetWithContext(c.Request.Context(), "/dedicated/server", &names)
		case "vps":
			err = client.GetWithContext(c.Request.Context(), "/vps", &names)
		}
		if err != nil {
			supportFailure(c, err, false)
			return
		}
		if names == nil {
			names = []string{}
		}
		sort.Strings(names)
		c.JSON(200, names)
	}
}

// ListSupportTickets 分页查询 ID 后有限并发读取详情,失败条目保留占位和错误。
func ListSupportTickets(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		acc, client, err := supportClient(state, c)
		if err != nil {
			return
		}
		page := 0
		if raw := c.Query("page"); raw != "" {
			page, err = strconv.Atoi(raw)
			if err != nil || page < 0 || page > 100000 {
				c.JSON(400, gin.H{"success": false, "error": "工单页码无效", "code": "SUPPORT_PAGE_INVALID"})
				return
			}
		}
		status := c.Query("status")
		if status != "" && status != "open" && status != "closed" {
			c.JSON(400, gin.H{"success": false, "error": "工单状态无效", "code": "SUPPORT_STATUS_INVALID"})
			return
		}
		q := url.Values{"page": {strconv.Itoa(page)}, "pageSize": {"20"}}
		subject := strings.TrimSpace(c.Query("subject"))
		if len([]rune(subject)) > 255 {
			c.JSON(400, gin.H{"success": false, "error": "工单搜索标题不能超过 255 个字符", "code": "SUPPORT_SEARCH_TOO_LONG"})
			return
		}
		if subject != "" {
			q.Set("subject", subject)
		}
		if status != "" {
			q.Set("status", status)
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
		defer cancel()
		var ids []int64
		if err = client.GetWithContext(ctx, "/support/tickets?"+q.Encode(), &ids); err != nil {
			supportFailure(c, err, false)
			return
		}
		hasMore := len(ids) >= 20
		if len(ids) > 20 {
			ids = ids[:20]
		}
		items := make([]map[string]interface{}, len(ids))
		var wg sync.WaitGroup
		sem := make(chan struct{}, 5)
		for i, id := range ids {
			sem <- struct{}{}
			wg.Add(1)
			go func(i int, id int64) {
				defer wg.Done()
				defer func() { <-sem }()
				item := map[string]interface{}{}
				var readErr error
				if ctx.Err() != nil {
					readErr = ctx.Err()
				} else {
					var isolated *ovhsdk.Client
					isolated, readErr = state.OVH.IsolatedClientFor(acc.ID)
					if readErr == nil {
						isolated.Timeout = 15 * time.Second
						readErr = isolated.GetWithContext(ctx, fmt.Sprintf("/support/tickets/%d", id), &item)
						if readErr == nil && idToString(item["ticketId"]) != strconv.FormatInt(id, 10) {
							readErr = errSupportDetailID
						}
					}
				}
				if readErr != nil {
					_, failure := supportErrorResponse(readErr, false)
					item = map[string]interface{}{"ticketId": id, "error": failure["error"], "code": failure["code"]}
					if params, exists := failure["params"]; exists {
						item["params"] = params
					}
				}
				items[i] = item
			}(i, id)
		}
		wg.Wait()
		failed := 0
		for _, item := range items {
			if item["error"] != nil {
				failed++
			}
		}
		c.JSON(200, gin.H{"accountId": acc.ID, "accountName": acc.Name, "items": items, "page": page, "hasMore": hasMore, "failedCount": failed})
	}
}

// supportTicketID 仅接受正数内部 ticketId,与对外展示 ticketNumber 区分。
func supportTicketID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("ticket_id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(400, gin.H{"success": false, "error": "工单内部 ID 无效", "code": "SUPPORT_TICKET_ID_INVALID"})
		return 0, false
	}
	return id, true
}

// GetSupportTicket 读取完整对话,不把消息读取失败当作空对话。
func GetSupportTicket(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := supportTicketID(c)
		if !ok {
			return
		}
		acc, client, err := supportClient(state, c)
		if err != nil {
			return
		}
		var ticket map[string]interface{}
		messages := []map[string]interface{}{}
		path := fmt.Sprintf("/support/tickets/%d", id)
		if err = client.GetWithContext(c.Request.Context(), path, &ticket); err != nil {
			supportFailure(c, err, false)
			return
		}
		if idToString(ticket["ticketId"]) != strconv.FormatInt(id, 10) {
			c.JSON(502, gin.H{"success": false, "error": "工单详情缺少或返回错误的内部 ID", "code": "SUPPORT_DETAIL_INVALID"})
			return
		}
		if err = client.GetWithContext(c.Request.Context(), path+"/messages", &messages); err != nil {
			supportFailure(c, err, false)
			return
		}
		if ticket == nil {
			ticket = map[string]interface{}{}
		}
		if messages == nil {
			messages = []map[string]interface{}{}
		}
		c.JSON(200, gin.H{"accountId": acc.ID, "accountName": acc.Name, "ticket": ticket, "messages": messages})
	}
}

// CreateSupportTicket 要求显式选择类别与产品,不发送已弃用 type 或付费支持专属优先级。
func CreateSupportTicket(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		var body struct {
			Subject     string `json:"subject"`
			Body        string `json:"body"`
			Category    string `json:"category"`
			Product     string `json:"product"`
			Subcategory string `json:"subcategory"`
			ServiceName string `json:"serviceName"`
		}
		if c.ShouldBindJSON(&body) != nil {
			c.JSON(400, gin.H{"success": false, "error": "工单参数格式不正确", "code": "SUPPORT_PAYLOAD_INVALID"})
			return
		}
		body.Subject = strings.TrimSpace(body.Subject)
		body.Body = strings.TrimSpace(body.Body)
		body.ServiceName = strings.TrimSpace(body.ServiceName)
		if body.Subject == "" || body.Body == "" || !supportMember(body.Category, supportCategories) || !supportMember(body.Product, supportProducts) || (body.Subcategory != "" && !supportMember(body.Subcategory, supportSubcategories)) {
			c.JSON(400, gin.H{"success": false, "error": "请填写标题和正文,并选择有效类别、产品及子类别", "code": "SUPPORT_FIELDS_INVALID"})
			return
		}
		acc, client, err := supportClient(state, c)
		if err != nil {
			return
		}
		payload := map[string]interface{}{"subject": body.Subject, "body": body.Body, "category": body.Category, "product": body.Product}
		if body.Subcategory != "" {
			payload["subcategory"] = body.Subcategory
		}
		if body.ServiceName != "" {
			payload["serviceName"] = body.ServiceName
		}
		var result map[string]interface{}
		if err = client.PostWithContext(c.Request.Context(), "/support/tickets/create", payload, &result); err != nil {
			supportFailure(c, err, true)
			return
		}
		createdID, idErr := strconv.ParseInt(idToString(result["ticketId"]), 10, 64)
		if idErr != nil || createdID <= 0 {
			c.JSON(502, gin.H{"success": false, "error": "OVH 已返回提交结果,但没有有效工单编号。请先刷新工单列表核对,避免重复提交。", "code": "SUPPORT_CREATE_UNCONFIRMED"})
			return
		}
		c.JSON(200, gin.H{"accountId": acc.ID, "result": result})
	}
}

// UpdateSupportTicket 重新读取状态再回复/关闭/重开;每次用户提交仅发送一次 POST。
func UpdateSupportTicket(state *app.State) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		id, ok := supportTicketID(c)
		if !ok {
			return
		}
		var body struct {
			Action string `json:"action"`
			Body   string `json:"body"`
		}
		if c.ShouldBindJSON(&body) != nil || (body.Action != "reply" && body.Action != "close" && body.Action != "reopen") {
			c.JSON(400, gin.H{"success": false, "error": "无效的工单操作", "code": "SUPPORT_ACTION_INVALID"})
			return
		}
		body.Body = strings.TrimSpace(body.Body)
		if body.Action != "close" && body.Body == "" {
			c.JSON(400, gin.H{"success": false, "error": "请填写回复或重新打开工单的理由", "code": "SUPPORT_BODY_REQUIRED"})
			return
		}
		_, client, err := supportClient(state, c)
		if err != nil {
			return
		}
		path := fmt.Sprintf("/support/tickets/%d", id)
		var ticket struct {
			State       string `json:"state"`
			CanBeClosed bool   `json:"canBeClosed"`
		}
		if err = client.GetWithContext(c.Request.Context(), path, &ticket); err != nil {
			supportFailure(c, err, false)
			return
		}
		if (body.Action == "close" && (ticket.State != "open" || !ticket.CanBeClosed)) || (body.Action == "reply" && ticket.State != "open") || (body.Action == "reopen" && ticket.State != "closed") {
			c.JSON(409, gin.H{"success": false, "error": "工单状态已变化或不允许此操作,请刷新后重试", "code": "SUPPORT_STATE_CONFLICT"})
			return
		}
		var payload interface{}
		if body.Action != "close" {
			payload = map[string]string{"body": body.Body}
		}
		switch body.Action {
		case "reply":
			err = client.PostWithContext(c.Request.Context(), path+"/reply", payload, nil)
		case "close":
			err = client.PostWithContext(c.Request.Context(), path+"/close", nil, nil)
		case "reopen":
			err = client.PostWithContext(c.Request.Context(), path+"/reopen", payload, nil)
		}
		if err != nil {
			supportFailure(c, err, true)
			return
		}
		c.JSON(200, gin.H{"status": "submitted"})
	}
}
