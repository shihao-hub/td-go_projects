package main

// main
//
// 需求：
// 	将 opencode sqlite 数据库中的全部会话的模型、思考深度等按模型和思考深度分组排序展示
// 	供应商、会话数量等信息也可以展示
//
// 面向过程：
// 	找到 opencode.db，打开，编写 sql，得到结果，组装成数据模型，渲染
//
// 面向对象（第一版）：
// 	sqlite.db 管理器
// 		openDB
// 		query
// 	TUI/CLI 渲染器
// 		渲染
// 	数据组装器
// 		组装 sql 返回数据
//
// 面向对象（第二版·领域模型·AI）：
// 	第一版只设计了工人（管理器/组装器/渲染器），第二版先定义零件，再让行为各归其位
//
// 	零件（被处理的东西，先有形状）：
// 		ModelSel     模型选择 = 供应商 + 模型名 + 思考档位
// 			GroupKey()         分组键 "provider/model/effort"
// 			EffectiveEffort()  合并 opencode 配置后真实生效的档位
// 		SessionStat  会话统计事实 = 创建时间 + 当前 ModelSel（+消息数/启动 ModelSel）
// 		Stats        分组排序后的视图 = 分组键 → 会话数，渲染的直接输入
//
// 	工人（处理零件的机器，依赖单向流动）：
// 		Store      读库：sql 行 → []SessionStat
// 		Aggregator 聚合：[]SessionStat → Stats（纯计算，不碰数据库，可单测）
// 		Renderer   渲染：Stats → io.Writer（不知道数据库存在，可造假数据测）
//
// 	接口缝：
// 		type SessionSource interface { Sessions(ctx) ([]SessionStat, error) }

func main() {

}
