DB_URL ?= postgres://relay:relay@localhost:5432/relay?sslmode=disable

.PHONY: run migrate-up migrate-down migrate-status sqlc build test

run: ## 启动服务
	go run ./cmd/server

build: ## 编译全部
	go build ./...

migrate-up: ## 应用所有未执行的迁移
	GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" goose -dir ./migrations up

migrate-down: ## 回滚一个迁移
	GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" goose -dir ./migrations down

migrate-status: ## 查看迁移状态
	GOOSE_DRIVER=postgres GOOSE_DBSTRING="$(DB_URL)" goose -dir ./migrations status

sqlc: ## 重新生成查询代码
	sqlc generate