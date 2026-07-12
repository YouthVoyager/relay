package store

import (
	"context"
	"fmt"
	"time"

	"github.com/YouthVoyager/relay/internal/store/sqlcgen"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store 持有数据库连接池,后续所有数据访问方法都挂在它上面。
type Store struct {
	Pool *pgxpool.Pool
	Queries *sqlcgen.Queries
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	pc, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	// ---- 生产级连接池参数 ----

	// 池上限。经验起点:够用就好,宁小勿大。pg 默认 max_connections=100,
	// 要为迁移工具、psql 调试、未来多实例部署留余量。
	pc.MaxConns = 10

	// 保底空闲连接:避免流量从零恢复时,每个请求都付一次建连成本(TCP+TLS+auth 几十 ms)。
	pc.MinConns = 2

	// 连接最长存活 30 分钟,到期由池子重建。
	// 作用:防止单条连接无限期持有(pg 侧内存碎片、配置变更不生效),
	// 也让负载均衡后的多个 pg 节点有机会重新均衡。
	pc.MaxConnLifetime = 30 * time.Minute

	// 关键:给寿命加抖动。否则启动时同批创建的连接会在同一瞬间集体过期、
	// 集体重建——制造周期性的延迟尖刺("惊群")。加抖动把重建摊开。
	pc.MaxConnLifetimeJitter = 5 * time.Minute

	// 空闲超过 5 分钟的连接回收(保留 MinConns 个)。
	// 防止流量高峰把池子撑满后,大量连接空占资源;
	// 也降低被中间网络设备(NAT/防火墙)静默掐断的空闲连接被复用的概率。
	pc.MaxConnIdleTime = 5 * time.Minute

	// 池子每分钟巡检一次:补齐 MinConns、清理过期/超时连接。
	pc.HealthCheckPeriod = time.Minute

	// 单次建连超时。没有它,pg 无响应时建连会长时间挂起。
	pc.ConnConfig.ConnectTimeout = 5 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// fail fast:启动时就验证数据库真的可达,而不是等第一个请求才发现。
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Store{
		Pool: pool,
		Queries: sqlcgen.New(pool),
	}, nil
}

// WithTx 在事务中执行 fn:fn 返回 nil 则提交,否则回滚。
// fn 拿到的 Queries 绑定在事务上——同一套查询,事务执行环境(DBTX 的兑现)。
func (s *Store) WithTx(ctx context.Context, fn func(q *sqlcgen.Queries) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // 提交后 Rollback 是无害的 no-op;panic/早退时它是保险丝

	if err := fn(sqlcgen.New(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) Close() {
	s.Pool.Close()
}