package ovh

import (
	"context"
	"net/http"
	"sync"

	ovhsdk "github.com/ovh/go-ovh/ovh"
)

// GetParallel 并发执行一批只读 GET。go-ovh 的 NewRequest 会改写共享 http.Client.Timeout,
// 所以先在当前 goroutine 依次签名,再以最多 limit 个并发发送;结果写入 target(i)。
// 返回按下标顺序的第一个错误。只用于只读请求:写操作的先后顺序有业务含义,不能这样并发。
func GetParallel(ctx context.Context, client *ovhsdk.Client, paths []string, limit int, target func(i int) interface{}) error {
	if limit < 1 {
		limit = 1
	}
	requests := make([]*http.Request, len(paths))
	for i, path := range paths {
		req, err := client.NewRequest(http.MethodGet, path, nil, true)
		if err != nil {
			return err
		}
		requests[i] = req.WithContext(ctx)
	}
	errs := make([]error, len(paths))
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			resp, err := client.Do(requests[i])
			if err == nil {
				err = client.UnmarshalResponse(resp, target(i))
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
