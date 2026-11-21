package store

// 迁移只做「向前加」，不删列不改列。
// 用 PRAGMA user_version 记版本，比自建 schema_version 表少一次往返。
const schemaVersion = 13

var migrations = [][]string{
	// v0 -> v1：初始结构
	{
		`CREATE TABLE IF NOT EXISTS hotspot (
			id           TEXT PRIMARY KEY,
			platform     TEXT NOT NULL,
			title        TEXT NOT NULL,
			author       TEXT NOT NULL DEFAULT '',
			url          TEXT NOT NULL DEFAULT '',
			heat         INTEGER NOT NULL DEFAULT 0,
			tags         TEXT NOT NULL DEFAULT '',
			published_at TEXT NOT NULL,
			fetched_at   TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_hotspot_fetched ON hotspot(fetched_at)`,

		`CREATE TABLE IF NOT EXISTS topic (
			id           TEXT PRIMARY KEY,
			title        TEXT NOT NULL,
			platform     TEXT NOT NULL DEFAULT '',
			niche        TEXT NOT NULL DEFAULT '',
			score        INTEGER NOT NULL DEFAULT 0,
			score_reason TEXT NOT NULL DEFAULT '',
			source       TEXT NOT NULL DEFAULT 'hotspot',
			status       TEXT NOT NULL DEFAULT 'draft',
			hotspot_id   TEXT NOT NULL DEFAULT '',
			heat         INTEGER NOT NULL DEFAULT 0,
			published_at TEXT NOT NULL DEFAULT '',
			created_at   TEXT NOT NULL,
			picked_at    TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_topic_status ON topic(status)`,
		`CREATE INDEX IF NOT EXISTS idx_topic_score  ON topic(score DESC)`,

		// 每次状态转移都留痕。现在几乎零成本，三期做复盘时没有它就是无米之炊。
		`CREATE TABLE IF NOT EXISTS topic_event (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			topic_id    TEXT NOT NULL,
			from_status TEXT NOT NULL,
			to_status   TEXT NOT NULL,
			at          TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_event_topic ON topic_event(topic_id)`,

		`CREATE TABLE IF NOT EXISTS job_run (
			id         TEXT PRIMARY KEY,
			flow_key   TEXT NOT NULL,
			status     TEXT NOT NULL DEFAULT 'queued',
			progress   INTEGER NOT NULL DEFAULT 0,
			err        TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			ended_at   TEXT
		)`,

		// 模型结果缓存：用户点「重新生成」和崩溃重跑都靠它省钱。
		`CREATE TABLE IF NOT EXISTS ai_cache (
			key        TEXT PRIMARY KEY,
			model      TEXT NOT NULL DEFAULT '',
			result     TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	},
	// v1 -> v2：hotspot 补赛道。
	// 没有它就没法在数据源挂掉时按赛道取本地缓存，降级链的第二级直接断掉。
	// 老行落空串，取缓存时按 fetched_at 兜底，不会误判。
	{
		`ALTER TABLE hotspot ADD COLUMN niche TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_hotspot_niche ON hotspot(niche, fetched_at)`,
	},
	// v2 -> v3：AI 分与「长期记忆」。
	//
	// ai_score 默认 -1 表示「这条没经过模型」，和「模型给了 0 分」必须能区分，
	// 否则复盘时会把两次不同口径的分数混在一起算。
	//
	// profile 单行：这是喂给模型的「你是谁」，一天填一次、之后基本不动。
	// 不做多行快照，也不做版本表——人在改人设时不需要历史版本，需要的是别丢。
	{
		`ALTER TABLE topic ADD COLUMN ai_score INTEGER NOT NULL DEFAULT -1`,
		`CREATE TABLE IF NOT EXISTS profile (
			id          INTEGER PRIMARY KEY CHECK (id = 1),
			niche       TEXT NOT NULL DEFAULT '',
			positioning TEXT NOT NULL DEFAULT '',
			audience    TEXT NOT NULL DEFAULT '',
			tone        TEXT NOT NULL DEFAULT '',
			avoid       TEXT NOT NULL DEFAULT '',
			updated_at  TEXT NOT NULL DEFAULT ''
		)`,
	},
	// v3 -> v4：界面状态。
	//
	// 为什么不塞进 profile：profile 是「喂给模型的数据」，用户改它会影响打分；
	// 这张表是「界面跑到哪一步了」，两者混在一行，将来改人设会顺手把引导状态
	// 一起覆盖掉。分开存，谁也不碰谁。
	//
	// 也不用 config.json：那是人可编辑的配置文件，程序往里写界面状态，
	// 用户手改一次就乱。
	{
		`CREATE TABLE IF NOT EXISTS app_setting (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		)`,
	},
	// v4 -> v5：排期。
	//
	// 原状态机里 scheduled 只是个没有日期的标签，日历和「今天该发什么」
	// 都没法做。plan_date 存 yyyy-mm-dd（本地日期，不带时区）：
	// 排期是「我打算 3 号发」，不是「3 号 00:00 UTC+8 这个时刻」，
	// 存日期串既省事又不会因为时区换算把日子挪走。
	//
	// 空串表示没排期，与「排了但日期未知」不同——后者不该存在，也就不留这种可能。
	{
		`ALTER TABLE topic ADD COLUMN plan_date TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_topic_plan ON topic(plan_date)`,
	},
	// v5 -> v6：成稿与发布台账。
	//
	// draft 与 topic 是一对多，不是一对一：同一选题换平台、换形态、换切口
	// 都要重写一稿，只留最新一稿等于把用户"我想试试另一个角度"这个动作删掉。
	// 所以稿件独立成表，用 topic_id 关联，谁最新由 created_at 决定。
	//
	// segments 存口播稿的分镜，是 JSON 文本。不拆成子表：
	// 分镜永远整段读、整段写，没有"查第三个镜头"这种需求，
	// 拆表只会多一次 join 和一处写入顺序的坑。
	//
	// edited 记「人改过没有」。它是后面判断"AI 第一稿到底能不能用"的唯一依据，
	// 不加这个字段，复盘时只能说"生成了 30 篇"，说不了"其中 4 篇一次没改"。
	{
		`CREATE TABLE IF NOT EXISTS draft (
			id         TEXT PRIMARY KEY,
			topic_id   TEXT NOT NULL,
			platform   TEXT NOT NULL DEFAULT '',
			format     TEXT NOT NULL DEFAULT 'long',
			title      TEXT NOT NULL DEFAULT '',
			body       TEXT NOT NULL DEFAULT '',
			tags       TEXT NOT NULL DEFAULT '',
			segments   TEXT NOT NULL DEFAULT '',
			note       TEXT NOT NULL DEFAULT '',
			edited     INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_draft_topic ON draft(topic_id, created_at DESC)`,

		// 发布台账。与 topic.status 的关系：状态是「这条选题走到哪了」，
		// 台账是「哪一天发到了哪个账号、效果如何」。同一选题可以发多个平台，
		// 状态只有一个，所以次数与效果必须单独记，否则复盘只能数选题数。
		//
		// 指标默认 -1 表示未回填，和「回填了 0」区分开：0 播放是要复盘的样本，
		// 未回填不是。混成 0 会让均值被一堆假零拉垮。
		`CREATE TABLE IF NOT EXISTS publish_record (
			id           TEXT PRIMARY KEY,
			topic_id     TEXT NOT NULL,
			draft_id     TEXT NOT NULL DEFAULT '',
			platform     TEXT NOT NULL DEFAULT '',
			account      TEXT NOT NULL DEFAULT '',
			url          TEXT NOT NULL DEFAULT '',
			published_at TEXT NOT NULL,
			note         TEXT NOT NULL DEFAULT '',
			views        INTEGER NOT NULL DEFAULT -1,
			likes        INTEGER NOT NULL DEFAULT -1,
			collects     INTEGER NOT NULL DEFAULT -1,
			comments     INTEGER NOT NULL DEFAULT -1,
			created_at   TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pub_topic ON publish_record(topic_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pub_at ON publish_record(published_at DESC)`,
	},
	// v6 -> v7：打分维度落库。
	//
	// 原来只存 score_reason（一段拼好的中文），四个维度算完就扔了。
	// 界面上要显示「热度 86 / 新颖 90 / 匹配 95 / 可写 88」这四条进度条时，
	// 从那段文字里反解是不可行的——文案随时会改，解析器就跟着碎。
	//
	// 存 JSON 而不是四个整数列：维度是打分层的事，将来加一维度（比如「争议度」）
	// 只改 score 包，不用再迁移一次表结构。读的时候按 name 取，取不到就不显示，
	// 老数据（空串）自然降级成「没有维度信息」，不会显示成四个 0。
	{
		`ALTER TABLE topic ADD COLUMN factors TEXT NOT NULL DEFAULT ''`,
	},
	// v7 -> v8：素材库。
	//
	// 素材和选题是两类东西：「这条选题我要写」和「这篇别人写得好，我存着学」。
	// 混进 topic 表会让选题池被学素材灌满，筛选就没法做了，所以独立成表。
	//
	// body 存用户粘进来的正文：微信和小红书都封了抓取，能拿到的只有用户自己贴的。
	// 没有 body 也能存（只有链接和标题），只是 AI 摘要做不了——那种情况下
	// summary 和 points 留空，界面显示「待提炼」，不是假装提炼过了。
	//
	// points 是 JSON 数组（值得学习的点，3 条）。和 draft.segments 一样拆成子表
	// 没有收益：它永远整段读、整段写。
	//
	// topic_id 是「这条素材能当哪个选题的参考」，一对一够用。多对多是以后的事，
	// 现在做关联表只会在没有查询需求时先付一次复杂度。
	{
		`CREATE TABLE IF NOT EXISTS material (
			id         TEXT PRIMARY KEY,
			url        TEXT NOT NULL DEFAULT '',
			platform   TEXT NOT NULL DEFAULT '',
			author     TEXT NOT NULL DEFAULT '',
			title      TEXT NOT NULL DEFAULT '',
			body       TEXT NOT NULL DEFAULT '',
			summary    TEXT NOT NULL DEFAULT '',
			points     TEXT NOT NULL DEFAULT '',
			likes      INTEGER NOT NULL DEFAULT -1,
			collects   INTEGER NOT NULL DEFAULT -1,
			status     TEXT NOT NULL DEFAULT 'raw',
			topic_id   TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_material_plat ON material(platform, created_at DESC)`,
	},
	// v8 -> v9：发布记录上加「复盘」三件套。
	//
	// 台账原先只到「这条发出去、数据多少」。少了最要紧的一步：自己怎么看它。
	// rating（1–5 星）是主观判断，views/likes 是客观结果，两者落在同一行上，
	// 才能回答「我觉得好的，观众认不认」——这是全程序里唯一能校准直觉的地方。
	// 分开存两张表就答不了这个问题，还得每次 join 回来。
	//
	// rating 默认 0 表示「还没复盘」。不用 NULL：星级最小是 1，
	// 0 不可能是真实评分，所以它当哨兵是安全的，读取时也不用管 NULL。
	//
	// analysis 是这一次的复盘（为什么好/为什么差），lesson 是抽出来的一句
	// 可复用经验。分两列而不是一列：「经验」要被单独捞出来反复用，
	// 跟某一条具体的笔记混在同一段文字里，就再也捞不出来了。
	{
		`ALTER TABLE publish_record ADD COLUMN rating INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE publish_record ADD COLUMN analysis TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE publish_record ADD COLUMN lesson TEXT NOT NULL DEFAULT ''`,
	},
	// v9 -> v10：拆解结果落库。
	//
	// 拆完就扔是一次性的：用户拆了十条，第二天想回头看「那个标题公式怎么写来着」，
	// 只能再拆一遍，还要再花一次模型调用。所以拆解必须有历史。
	//
	// 不复用 material 表：素材是「这篇别人写得好，我留着学」（带作者、平台、正文），
	// 拆解是「这条的结构是什么」（六行字段 + 可借鉴点）。两者的列表、筛选、
	// 操作完全不同，塞进一张表就得为每一行记住「你是哪种」，查询和界面都要分叉。
	//
	// result 存整份 JSON 而不是七个列：字段是模型输出的一部分，会随提示词调整而变。
	// 一列一个字段的话，每次改提示词都要迁移一次表结构，而历史数据又没法回填。
	// category 单独冗余一列，只为了列表能按类型分组而不必解析 JSON。
	{
		`CREATE TABLE IF NOT EXISTS teardown (
			id         TEXT PRIMARY KEY,
			title      TEXT NOT NULL DEFAULT '',
			body       TEXT NOT NULL DEFAULT '',
			result     TEXT NOT NULL DEFAULT '',
			category   TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_teardown_at ON teardown(created_at DESC)`,
	},
	// v10 -> v11：账号。
	//
	// 单行表。id 就是机器码——「注册」这件事在本机完成：
	// 第一次打开时按机器码建一行，用户看到的是一个能改的名字和手机号。
	//
	// 为什么不做成多用户：这是买断的单机工具，一台机器两个人用同一边界的可能性
	// 远小于「多一个账号体系导致每次启动都要选人」。真要多人，各用各的电脑。
	//
	// 姓名/手机号允许为空：首启动不拦人。空的时候界面提示去补，
	// 但不填也能把整条链路走完——试用期最怕的就是被表单挡住。
	//
	// synced_at 现在恒为空，是给将来接服务端留的：同步成功后写时间戳，
	// 用来判断本地资料和服务端谁更新。加一列的成本远低于将来改表。
	{
		`CREATE TABLE IF NOT EXISTS account (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL DEFAULT '',
			phone      TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			synced_at  TEXT NOT NULL DEFAULT ''
		)`,
	},
	// v11 -> v12：指标来源。
	//
	// 同一个 views 字段，可能来自平台自动抓取，也可能是用户手抄的。
	// 两者的可信度不一样：手抄会抄错、会抄成前天的数、会因为懒得翻而不抄。
	// 界面上必须能告诉用户「这个数是我从平台拿的，还是你自己填的」，
	// 否则他没法判断该不该相信那一列——所以来源要落库，不能靠猜。
	//
	// metrics_src 用字符串而不是布尔：将来可能多一个来源（比如官方接口、
	// 或者从另一个工具导入）。布尔值到时就得再加一列。
	// metrics_at 记最近一次自动取数的时间——平台的数据是滞后的，
	// 用户看到「3 小时前同步」和「3 天前同步」时对同一组数的反应完全不同。
	{
		`ALTER TABLE publish_record ADD COLUMN metrics_src TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE publish_record ADD COLUMN metrics_at TEXT NOT NULL DEFAULT ''`,
	},
	// v12 -> v13：发布任务。
	//
	// 三段式发布（选类型 → 选素材和账号 → 生成任务）产出的是一串「待办」，
	// 和 publish_record 不是一类东西：那个是账（已经发生），这个是计划（还没发生）。
	// 合进一张表就得加状态位，之后每个统计都要记得过滤一次，迟早漏。
	//
	// 存 platform/format 而不是只存 draft_id：草稿可以改平台、改形态，
	// 甚至被删掉。任务是「当时决定发到哪」，要能独立站住——
	// 草稿没了，这条任务还该显示「稿子已删」而不是整条消失。
	//
	// account 存名字而不是 id：账号表目前是单行（见 v11 的说明），
	// 存名字才能和台账里手填的账号对得上。
	{
		`CREATE TABLE IF NOT EXISTS pub_task (
			id         TEXT PRIMARY KEY,
			draft_id   TEXT NOT NULL DEFAULT '',
			topic_id   TEXT NOT NULL DEFAULT '',
			title      TEXT NOT NULL DEFAULT '',
			platform   TEXT NOT NULL DEFAULT '',
			format     TEXT NOT NULL DEFAULT '',
			account    TEXT NOT NULL DEFAULT '',
			status     TEXT NOT NULL DEFAULT 'todo',
			seq        INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_status ON pub_task(status, seq)`,
	},
}
