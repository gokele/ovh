package purchase

import (
	"context"
	"fmt"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
	"github.com/ovh-buy/server/internal/price"
	"github.com/ovh-buy/server/internal/types"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

type cartSessionKey struct{}
type stockSignalKey struct{}

// cartSession 只由所属任务的执行 goroutine 使用,不会跨任务共用购物车或 SDK 可变状态。
type cartSession struct {
	nextPrepare  time.Time
	base, client *ovhsdk.Client
	accountID    string
	account      types.OVHAccount
	cart         *preparedCart
	empty        *emptyCart
	partial      *partialCart
	sale         saleSnapshot
	// checkoutClear: 本会话已确认没有结账登记,且之后本会话未再登记。
	checkoutClear bool
}

type partialCart struct {
	result                      map[string]interface{}
	client                      *ovhsdk.Client
	intent, fqn, duration, mode string
	itemID                      int64
	options                     []string
	modes                       map[string]string
	expires                     time.Time
}

// Retained only after OVH confirms the rejected base-item request left no items.
type emptyCart struct {
	result  map[string]interface{}
	client  *ovhsdk.Client
	intent  string
	expires time.Time
}

type preparedCart struct {
	preparedAt                         time.Time
	client                             *ovhsdk.Client
	account                            types.OVHAccount
	call                               *ovh.PreparedCall
	quote                              *price.PriceInfo
	options                            []string
	modes                              map[string]string
	stockOptions                       []string
	orderOptions                       []string
	intent                             string
	duration                           string
	id, fqn, subsidiary, couponWarning string
	expires                            time.Time
}

// clientFor 复用已经校准的 SDK 实例;凭据或代理更新使工厂 base 改变时销毁旧准备。
func (s *cartSession) clientFor(state *app.State, account string) (*ovhsdk.Client, error) {
	acc, ok := state.FindAccount(account)
	if !ok {
		return nil, fmt.Errorf("OVH account not found: %s", account)
	}
	base, err := state.OVH.ClientForAccount(acc)
	if err != nil {
		return nil, err
	}
	if s.base != base || s.account.Zone != acc.Zone {
		s.discard()
		s.nextPrepare = time.Time{}
		client, err := ovh.IsolateClient(base)
		if err != nil {
			return nil, err
		}
		s.base, s.client = base, client
		s.accountID, s.account = acc.ID, acc
	}
	return s.client, nil
}

// discard 只清理尚未发送 checkout 的购物车;发送结果不确定的车已从 session 脱离。
func (s *cartSession) discard() {
	s.sale = saleSnapshot{}
	if s.partial != nil {
		id, _ := s.partial.result["cartId"].(string)
		(&preparedCart{id: id, client: s.partial.client}).cleanup()
		s.partial = nil
	}
	if s.cart != nil {
		s.cart.cleanup()
		s.cart = nil
	}
	if s.empty != nil {
		id, _ := s.empty.result["cartId"].(string)
		(&preparedCart{id: id, client: s.empty.client}).cleanup()
		s.empty = nil
	}
}

func cartExpires(raw interface{}) time.Time {
	now := time.Now()
	if value, ok := raw.(string); ok {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			expires := parsed.Add(-time.Minute)
			if cap := now.Add(30 * time.Minute); expires.After(cap) {
				expires = cap
			}
			return expires
		}
	}
	return now.Add(5 * time.Minute)
}

// cleanup 有界清理废弃准备;不沿用用户已取消的 context,也不无限阻塞任务退出。
func (c *preparedCart) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.client.DeleteWithContext(ctx, "/order/cart/"+c.id, nil)
}

// Rejected stale carts are cleaned without delaying a fresh checkout or sharing SDK mutation.
func (c *preparedCart) cleanupInBackground() {
	client, err := ovh.IsolateClient(c.client)
	if err != nil {
		return
	}
	client.Timeout = 5 * time.Second
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.DeleteWithContext(ctx, "/order/cart/"+c.id, nil)
	}()
}
