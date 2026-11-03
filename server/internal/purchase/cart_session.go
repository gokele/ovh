package purchase

import (
	"context"
	"time"

	"github.com/ovh-buy/server/internal/app"
	"github.com/ovh-buy/server/internal/ovh"
	ovhsdk "github.com/ovh/go-ovh/ovh"
)

type cartSessionKey struct{}
type stockSignalKey struct{}

// cartSession 只由所属任务的执行 goroutine 使用,不会跨任务共用购物车或 SDK 可变状态。
type cartSession struct {
	nextPrepare  time.Time
	base, client *ovhsdk.Client
	accountID    string
	cart         *preparedCart
}

type preparedCart struct {
	preparedAt                         time.Time
	client                             *ovhsdk.Client
	call                               *ovh.PreparedCall
	id, fqn, subsidiary, couponWarning string
	expires                            time.Time
}

// clientFor 复用已经校准的 SDK 实例;凭据或代理更新使工厂 base 改变时销毁旧准备。
func (s *cartSession) clientFor(state *app.State, account string) (*ovhsdk.Client, error) {
	base, err := state.OVH.ClientFor(account)
	if err != nil {
		return nil, err
	}
	if s.base != base {
		s.discard()
		s.nextPrepare = time.Time{}
		client, err := ovh.IsolateClient(base)
		if err != nil {
			return nil, err
		}
		s.base, s.client = base, client
		acc, _ := state.FindAccount(account)
		s.accountID = acc.ID
	}
	return s.client, nil
}

// discard 只清理尚未发送 checkout 的购物车;发送结果不确定的车已从 session 脱离。
func (s *cartSession) discard() {
	if s.cart != nil {
		s.cart.cleanup()
		s.cart = nil
	}
}

// cleanup 有界清理废弃准备;不沿用用户已取消的 context,也不无限阻塞任务退出。
func (c *preparedCart) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.client.DeleteWithContext(ctx, "/order/cart/"+c.id, nil)
}
