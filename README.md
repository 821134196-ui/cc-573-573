# 园区团餐配送批次与收餐核对系统

厨房、配送员、企业前台对每个餐次的**装箱 → 送达 → 签收**数量逐环节核对；少餐/错餐沿原批次发起补送或处置，全程留痕，并保证餐盒数量不凭空增加。

- 后端：Go + Gin + GORM + SQLite（纯 Go 驱动 modernc，无需 cgo）
- 前端：Vue 3 + Vite
- 配送调度、支付退款均为**本地模拟服务**（`backend/svcs/mocks.go`）
- 一条命令启动：`./run.sh`

## 启动

```bash
./run.sh              # 首次自动 npm install + 构建前端，然后启动服务
# 打开 http://localhost:8080
```

演示账号（登录页下拉选择，无密码）：

| 角色 | 账号 | 能做什么 |
|---|---|---|
| 厨房 | 周大厨 | 按企业/餐次订单生成装箱批次、登记保温箱封签、交接 |
| 配送员 | 王骑手 / 李骑手 | 交接接货、途中改派、送达、登记封签破损/无人签收 |
| 企业前台 | 陈前台（星云科技）/ 林前台（云帆网络） | 签收核对应收/实收/差额、提出差异 |
| 复核员 | 赵复核 | 差异复核（补送/损失核销/退款两阶段处理） |

重置演示数据：`rm backend/campusmeal.db` 后重启。

## 验证

```bash
cd backend && go test ./..          # 10 个单元/流程测试
python3 scripts/e2e_check.py         # 22 项 HTTP 端到端检查（自起服务、独立 e2e.db）
```

## 核心业务规则与落点

**数量守恒（每个批次恒成立）**

```
装箱数 packed = 签收数 signed + 核销/损失 lost + 在途待结 outstanding(≥0)
```

- 所有物理盒数变动都在 `box_ledgers` 留流水（装箱 +、签收 −、损失 −），`GET /api/audit` 同时核对汇总字段与流水余额，并校验补送批次都有已处理差异依据、退款回执全局唯一。
- 签收时任何菜品实收 > 应收一律 422 拒绝，清单外菜品不能出现 → 餐盒无法凭空增加。
- 补送批次 2 盒时，原批次少的 2 盒按损失核销，厨房新做的 2 盒在补送批次入账：物理总量守恒。

**沿原批次处理，不另建订单**

- 补送批次 `root_batch_id` 指回原批次，同企业同餐次，`kind=resupply`，每一盒都挂在一张已复核批准的差异单上（`box_ledgers.ref_type=discrepancy`）。

**权限分离**

- 装箱清单、封签、装箱数只有厨房可写；前台只可签收和提差异（签收/差异接口不接收任何装箱数量字段，且限本企业批次）。

**差异两阶段复核**

1. 复核员审批：批准并确定 `resupply / loss / refund`，处置数量必须与差异数量**逐盒相等**（或驳回）；
2. 执行：发起补送批次 / 损失核销 / 退款。

**退款幂等**

- 退款以支付回执号为幂等键：业务表 `refunds.receipt_no` 唯一索引 + 模拟支付网关双重去重。重复提交同一回执只返回去重标记，支付侧回执 `call_count` 增加但金额只入账一次；跨差异复用回执直接拒绝。

**路线唯一与留痕**

- `route_assignments` 对 `active` 路线有部分唯一索引：同一批次同时只有一条 active 路线；改派先结束旧路线（记录结束时间与原因）再建新路线，并在模拟配送服务留下 `dispatch → reroute → complete` 轨迹。
- 封签破损、无人签收、交接、改派、送达、签收、复核、处理、批次关闭全部写入 `events` 事件流。

## 主要 API

```
POST /api/batches                         厨房按订单装箱
POST /api/batches/:id/seals               登记保温箱封签（盒数合计须=装箱数）
POST /api/batches/:id/handover            交接给配送员+路线
POST /api/batches/:id/reroute             途中改派
POST /api/batches/:id/deliver             送达
POST /api/batches/:id/seal-broken         封签破损留痕
POST /api/batches/:id/nobody              无人签收留痕
POST /api/batches/:id/sign                前台签收（应收/实收/差额核对）
POST /api/batches/:id/discrepancies       前台提出差异
POST /api/discrepancies/:did/review       复核审批
POST /api/discrepancies/:did/resupply     执行补送（生成挂原批次的补送批次）
POST /api/discrepancies/:did/loss         执行损失核销
POST /api/discrepancies/:did/refund       执行退款（回执号幂等）
GET  /api/audit                           全局数量守恒对账
GET  /api/mock-logs                       模拟支付/配送服务记录
```

所有写接口通过请求头 `X-User-Id` 标识当前用户并做角色鉴权。

## 目录

```
backend/
  models/models.go    领域模型（批次/装箱/封签/路线/事件/签收/差异/损失/退款/餐盒流水）
  service.go          事务化业务逻辑与状态机
  queries.go          批次详情与全局守恒对账
  handlers.go         Gin 路由
  svcs/mocks.go       支付、配送本地模拟（同事务落库）
  service_test.go     数量守恒与关键流程测试
frontend/src/views/   厨房/配送员/前台/复核员/对账 五个工作台
scripts/e2e_check.py  端到端走查
```
