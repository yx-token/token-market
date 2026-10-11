# "真模型 / 没掺水"黑盒检测调研：面向官方订阅账号寄售的质检项设计

> 整理日期：2026-10-12。证据访问日期、数字与来源编号沿用原报告；来源编号仅在本报告内有效，不能与另三份报告混用。论文/官方页面的声明是该来源的一手事实，不等于对 token-market 真实账号的实测；【推断】与候选设计均未获真实账号验证。
>
> 本文保留威胁模型、论文数字、候选 A/D/S 清单及 R/V/T 来源登记。原报告中的“约 55 项删除、约 8 项自检、约 26 项保留”是重叠估计，不能作为 86 项的排他总数；逐项取舍与准确计数以[汇总报告](../quality-inspection-2026-10-12.md)为准。A07、A10、共用检测、平台自检等仍属候选或后续，不自动进入本期扣价。文中不适用的有害请求和注入串改为类别描述，保留其编号、来源和不采用的理由。不得从研究建议推导修改 sub2api 的任务。
>
> 实施优先级：[确认后的汇总](../quality-inspection-2026-10-12.md)与[数据契约](data-contract.md)高于本文的候选建议。所有业务、迁移、测试和脚本只能在 token-market 一侧实现，通过现有 Admin API、用户 API、网关、只读数据库与配置驱动 sub2api；能力缺口记为“依赖 sub2api，后续处理”，不修改 sub2api 的任何代码。


- 检索与访问日期：**2026-10-11**（下文所有 URL 均为该日访问）。
- 本文是**资料调研 + 质检项设计**，没有对任何真实账号发起探测，也没有运行任何下载来的代码；克隆的仓库只在 调研期间的只读克隆（临时材料，未入库） 下只读查看。
- 与前一份调研 [2026-10-10 降智调研](../model-degradation-gpt-claude-2026-10-10.md)（下称"前文"）的分工：前文已经覆盖统计判定（配对差异、聚类标准误、序贯检验、A/A 与阳性对照）、官方事故复盘、LiveNerf / Marginlab、B3IT、Logprob Tracking、Behavioral Fingerprints、能力题库（IFEval/RULER/LiveBench 等）。**本文不重复这些内容**，只在需要时引用"见前文 §x"。本文聚焦：**API 背后到底是不是那个模型 / 那个档位（真伪）**，以及**是不是给足了（掺水）**。
- 证据等级标注：**【一手】**= 本次读过论文全文/方法节、官方原文或源码；**【二手】**= 只看到摘要、转述、社区帖子或无法复核的说法。

## 0. 结论先行

1. **我们的场景决定了重点**：出口代理只转发 TLS 字节流，sub2api 上游连接禁止跳过证书校验（`pkg/httpclient/pool.go:126-129`），卖家改不了内容、换不了模型。所以 BazaarLink 86 项里，家族识别、自报身份、供应链/注入、回放缓存等约 55 项不需要对卖家账号跑，约 8 项改为平台自检，约 26 项改造后保留（§8）。真正的风险是三件事：**档位虚报**、**他人共用**、**官方按账号信任度回退/改路由**。
2. **2026 年官方"降智"已有一手、可观测的形态**（§3）：
   - Anthropic：Opus 5/5.5 的网络安全类请求回退到 Opus 4.8，生物与前沿 LLM 开发类回退到 Opus 5，蒸馏类直接拦截。分类器"factoring in a range of account trust signals"，回退会粘在整个会话上。
   - OpenAI：Codex 提示原文 "Your account was flagged for potentially high-risk cyber activity and this request was routed to gpt-5.2"，响应头 `OpenAI-Model` 能看到实际模型。
   - 智谱、Kimi 有公开的官方自动路由，应列入白名单，不能判为降智。
3. **最便宜也最硬的信号是被动的**（§5、§7）：响应型号字段与回退/拒答字段、订阅额度利用率响应头与不耗额度的用量接口、窗口起点、刷新令牌复用、公共前缀的异常缓存命中。它们几乎零成本，能覆盖真伪与共用的大部分。缺口是 sub2api v0.2.14 **不解析 `OpenAI-Model`，也取不到 Claude 的 Pro/Max 档位**。
4. **主动探针排优先级**：
   - 确定性项：输入计数指纹，外加档位、上下文、功能、参数规则这类"接受还是拒绝"的边界探针。
   - 知识边界指纹 KBF：论文报告 16 个同参考对照 0 误报、替换率 15% 时检出 95%，用来做"双胞胎复核"。
   - 能力与推理量配对，统计方法见前文。
   - 风格分类、MET 是研究中的辅助或二阶段候选；自报身份不作为本期认证、门槛或扣价判据。
5. **探针本身有风险**（§3.1、§3.2、§4.2）：
   - 有害内容、越狱串、要求复述推理过程的提示会被拦截，有的拦截照样计费，还可能触发风控。BazaarLink 自己的开源引擎记录过旧探针导致账号被停权。
   - "同一套题在大量账号上同步重复发"，正是 Anthropic 描述的协同异常。
   - 所以题目要参数化、轮换、低频，请求形态要和真实客户端一致。
6. **做不到或做不准的**：
   - 量化这类细微替换，以及只换一小部分请求的随机掉包，参见 Cai 等的负面结论。
   - 官方全局变化：被测账号和基线账号会一起变，必须保留历史锚点。
   - Opus 5.5 与它的回退目标 Opus 4.8 共用 tokenizer，计数分不出；GPT-5.x 全系用同一个 tokenizer。
   - 中文社区的"玄学题"（鹈鹕关键词、juice 等）。
7. **建议落地顺序**：
   1. 补采集字段：`OpenAI-Model`、拒答/回退字段、输入计数三项、限额头，以及 Claude 的 profile/bootstrap 档位。
   2. 上线被动项：A01/A02/A12/A13/A14、D01–D05、D13/D14。
   3. 上线确定性主动项：A03/A04/A05/A06/A11/A15。
   4. 建两个以上同型号基线账号并完成 A/A 校准，再上统计类：A07/A09/D06/D07/D10。
   5. 最后上 A10、D09 和平台自检 S 类。

   **实施取舍**：上述顺序是历史研究候选，不是本期任务清单。D09 已列入本期完整质检；A07/A10、共用检测、平台 S 类自检和超额开关列为后续。Claude profile/bootstrap 与 `OpenAI-Model` 目前不能由约定的现有接口取得，记“依赖 sub2api，后续处理”；不补写 sub2api、不直连厂商、不绕过凭据保管边界。

---

## 1. 先把威胁模型说清楚：我们的场景和"公开中转站"不一样

BazaarLink、api-relay-audit 这类工具面对的是"买家 → 不可信中转站 → (也许是)官方"的链路，中转站终止 TLS、能读写明文，所以要查偷换模型、改回复、注入、偷 key。arXiv 2604.08407 把这类攻击形式化为 AC-1/AC-2（见 §2.1）。

我们的链路是：买家 → 平台 Edge/sub2api（平台自己）→ 卖家提供的出口代理（HTTP CONNECT / SOCKS5，只转发 TLS 字节流）→ 官方固定 base_url。卖家只提供两样东西：**官方凭据**（OAuth 授权或 Coding Plan key）和**出口代理**。由此：

| 风险来源 | 具体形态 | 在我们场景的可能性 | 应该用什么查 |
|---|---|---|---|
| 卖家改写内容（AC-1 注入、AC-1.a 依赖劫持、AC-1.b 条件投递） | 改 tool call、改安装命令、按关键词注入 | **基本不可能**：出口代理看不到明文。前提是网关**强制校验证书**、不信任卖家给的 CA、不允许卖家改 base_url/Host | 不做逐号探针；做**平台自检**（证书校验、代理只走 CONNECT、配置审计） |
| 卖家偷窃凭据/密文（AC-2） | 扫 key、偷 prompt | 同上，代理拿不到明文；但**卖家本人持有该账号**，可以在官方后台看到对话历史（视产品而定）——这是隐私问题，不是模型真伪问题 | 账号隐私设置（`account_meta` 已有），在买家侧披露 |
| 卖家偷换模型 / 用便宜模型冒充 | 自建中转冒充官方 | **不可能**（base_url 固定官方、TLS 端到端） | 不需要"家族识别"类探针 |
| **卖家虚报档位** | Pro 冒充 Max、Max 5x 冒充 20x、Plus 冒充 Pro、Lite 冒充 Max Coding Plan | **可能** | 档位核验（凭据声明 + 配额/用量头 + 模型权限矩阵 + 上下文上限），见 §5、§6 |
| **账号被他人共用** | 卖家自用、同号多平台挂售、号池、凭据泄露被别人并入中转链 | **值得优先关注【推断】**：2604.08407 的投毒实验显示泄露的 key 很快被并入中转链（§2.1），但没有测量本平台卖家共用发生率 | 用量窗口漂移/窗口起点/用量对账/429 画像/刷新令牌复用/异常缓存命中（基本零成本，见 §7 D 类） |
| **官方按请求/账号改路由或降级** | 安全分类器按请求回退到弱一档模型（参考账号信任信号、会话粘性）；Codex 账号被标记后改路由到 gpt-5.2；风控后要求身份验证或限并发。**达到限额时官方是报错，不是悄悄换小模型**（§5.1） | **有一手证据**（见 §3） | 响应 `model` 字段 / `OpenAI-Model` 头、拒答与回退字段、无害对照题上的回退率、知识边界指纹（KBF/HSS）、能力配对；tokenizer 计数只能分新旧两代 |
| 官方压低推理、截短上下文 | effort 不被尊重、上下文小于宣称 | 针对单个账号的一手证据仍不足（前文 §1、§8 结论不变）；**档位差异**造成的上下文/effort 差异有一手证据 | reasoning/output token 配对、上下文上限与 input_tokens 核对 |
| 出口代理质量 | 慢、断流、IP 被风控 | 可能 | TTFT/TPS/错误率配对、代理诊断（已有） |
| **平台网关自身** | sub2api 的模型映射配置错误、为 OAuth 注入的系统提示、弱口令被入侵 | 平台内控问题，2604.08407 实测到弱口令 Sub2API 被并入中转链 | 配置审计 + 自检探针，**结果不计入卖家分** |

结论：BazaarLink 86 项里**安全/供应链类 16 项**和若干"中间层完整性"项在我们这里不需要逐号跑（逐项取舍见 §8），而它**完全没有覆盖**的"档位核验"和"共用检测"恰恰是我们的主风险。

---

## 2. 学术工作（前文未覆盖部分）

### 2.1 arXiv 2604.08407：BazaarLink 引用的攻击分类到底是什么【一手】

- **标题**：*Your Agent Is Mine: Measuring Malicious Intermediary Attacks on the LLM Supply Chain*
- **作者**：Hanzhi Liu、Chaofan Shou、Hongbo Wen、Yanju Chen、Ryan Jingyang Fang、Yu Feng（UCSB / UCSD / Fuzzland 等）；arXiv v1 2026-04-09；HTML 版页眉标注 ACM CCS 2026。
- 来源：https://arxiv.org/abs/2604.08407 ，全文 https://arxiv.org/html/2604.08407v1

**攻击分类（Table 1、§4）**

| 类别 | 含义 | 关键点 |
|---|---|---|
| AC-1 响应侧载荷注入 | 中转站改写上游返回的 tool call 参数（保持 schema 合法） | 例：把安装脚本 URL 换成攻击者地址；客户端看不到上游原文 |
| AC-2 被动秘密外泄 | 不改内容，只扫描请求/响应里的 key、私钥、token 并另行外传 | 例：`sk-…`、`AKIA…`、`ghp_…`、PEM 私钥正则 |
| AC-1.a 依赖定向注入 | 只改包名（typosquat），如 `requests`→`reqeusts` | 能绕过"只允许某些域名"的策略闸 |
| AC-1.b 条件投递 | 只在满足触发条件时注入 | 五类触发：工具名、内容关键词、用户指纹（如 YOLO 自动批准模式）、时间窗、**请求计数阈值**（如前 50 次正常） |

**测量结果（§5，Table 3）**：从淘宝、闲鱼、Shopify 买了 28 个付费中转，另收集 400 个免费中转（**主要基于 sub2api 与 new-api 模板**）。1 个付费 + 8 个免费中转主动注入恶意代码；2 个使用自适应触发（"前 50 次调用后才注入""只对 YOLO 模式且 Rust/Go 项目注入"）；17 个触碰了研究者布置的 AWS 蜜罐凭据；1 个盗走 ETH。投毒实验：故意泄露的 1 个 OpenAI key 被消耗约 1 亿 GPT-5.4 token；布置的**弱口令 Sub2API / CLIProxyAPI / claude-relay-service 诱饵**被并入他人中转链，承载约 20 亿 token、440 个 Codex 会话、暴露 99 个凭据，其中 401 个会话处于自动批准模式。

**作者自己承认的边界**：条件投递使"有限次黑盒审计在根本上不充分"（§4.2.2："A router deploying a 50-request warm-up appears entirely transparent during a 10-request probe"）。防御只能做客户端策略闸、异常检测（IsolationForest，6.7% 误报下检出 AC-1 89%、AC-1.a 50%、AC-1.b 50.9%）和追加式透明日志；长期方案是"官方签名的规范化响应信封"，目前没有厂商提供（§8.2）。

**对我们的意义**：
1. 卖家侧做不了 AC-1/AC-2（拿不到明文），所以 BazaarLink 依据这篇论文设计的依赖劫持、markdown 外泄、条件注入、canary 回传等探针**不应计入卖家质检**。
2. 但**平台自己就是一个 sub2api 中转**：论文明确把 sub2api / new-api 模板列为被测对象，并实测弱口令 Sub2API 被入侵。平台要对买家证明"我们没改你的内容"，可借鉴论文的透明日志（响应字节哈希 + 时间 + 上游 request-id），这是平台信任问题，不是卖家质检项。
3. "泄露凭据很快被并入中转链"直接对应我们的**共用风险**：卖家的 OAuth 凭据如果同时出现在别的号池里，账号会被并发消耗。

### 2.2 Model Equality Testing（Gao、Liang、Guestrin，ICLR 2025）【一手】

- arXiv 2410.20247 v2（2025-04-08，ICLR 2025 camera-ready）；https://arxiv.org/html/2410.20247v2 ；代码与 100 万条补全数据：https://github.com/i-gao/model-equality-testing
- **检测什么**：API 的输出分布 Q 是否等于参考分布 P（量化、水印、微调、换模型、解码参数被改）。
- **方法**：两样本核检验，统计量为 MMD（最大均值差异），核用**Hamming 核**（两条补全逐位置相同 token 的个数，短的右侧补齐）。p 值用重复从 P 采样模拟零分布，或置换检验。
- **探针**：m=25 个随机 100 字符维基百科片段（英/德/西/法/俄），提示词 "Continue the paragraph. Do not output anything except the continuation to the paragraph. Start the continuation immediately."；补全长度 L=50 token；**temperature=1**；每侧 N=10m=250 条样本。闭源 API 只返回文本时在字符空间比较。
- **结果**：平均每个提示 10 个样本时，对量化/水印等扰动的中位检验功效 77.4%；fp16 难以检出；温度不同（0.5 或 1.5 vs 1）检出功效约 100%（附录 C.3）；不同模型之间几乎都能 100% 区分，但**同家族近邻较难**（Llama-3.1 8B vs 70B 功效 0.75），**字符空间里 int8 量化只有 0.07 功效**。实测 2024 夏 9 家供应商 31 个 Llama 端点，11 个与 Meta 参考权重分布不同；单次审计成本 < 1 美元（最贵端点 Wikipedia 任务 0.14 美元）。
- **在我们场景怎么用**：把 P 换成**同时段的基线账号**，Q 是被测账号，同模型、同请求模板、temperature=1（推理模型本来就固定 1）。原假设是"两个账号拿到的是同一个服务"。拒绝原假设只说明"分布不同"，**不说明谁更差**；官方本身的硬件/批处理不确定性也可能制造差异（前文 S17、S30；见 §2.3）。适合当"二阶段触发器"，不适合单独定罪。
- **成本**：25 提示 × 10 样本 × 2 侧 = 500 次请求，每次约 60 输入 + 50 输出 token，合计约 5.5 万 token；对订阅窗口不算大，但请求数多，频率要低（上架、申诉、被动信号异常时）。

### 2.3 "Are You Getting What You Pay For? Auditing Model Substitution in LLM APIs"（Cai、Shi、Zhao、Song）【一手】

- arXiv 2504.04715 v2（2025-09-29）；https://arxiv.org/html/2504.04715v2
- **它的价值主要是负面结论**——哪些方法在对抗条件下会失效：

| 方法 | 论文结果 |
|---|---|
| 文本分类器（BERT/T5/GPT-2/LLM2Vec，各 1 万样本训练） | 区分原模型与 FP8/INT8 量化版：**准确率约 50%（等于瞎猜）** |
| 身份自报（"Who are you?"，500 次/模型） | 能说出家族/厂商，但具体规格成功率 **< 5%**；只要系统提示词冒名，正确率**接近 0** |
| MMD（Hamming 核） | 小模型：即使**完全**换成 INT8 也测不出；大模型：随机掉包比例 p≥0.6 才能稳定拒绝，**p≤0.2 时功效跌回 α** |
| 基准题正确率 | 量化前后差异多在一个标准差内；温度不公开时，温度造成的下降与量化相当，无法归因 |
| 贪心解码逐 token 比对 | 同一 Gemma-2-9B 在不同供应商之间前 20/100 token 一致率 **< 5%**（推理不确定性） |
| logprob 比对 | 生产环境批处理不确定性导致误报 |

- 作者结论：纯软件黑盒方法对**细微**替换（量化、小比例随机掉包、对审计请求单独放行）不可靠，主张 TEE 硬件证明。
- **对我们的启示**：
  1. 量化/精度降级这种"细微替换"，我们黑盒几乎测不出——**不要承诺**能测。好在它更可能是官方全局行为，不是针对单个账号；同时段基线账号会一起受影响（前文 §5.1）。
  2. 跨型号替换（Opus→Sonnet/Haiku、Codex→mini/旧版）差异大得多，能力配对、知识边界指纹（§2.6、§2.7）、tokenizer 计数（§6）可以抓。
  3. **只对部分请求替换**（例如只有被安全分类器命中的请求才改路由）会让抽样探针漏检——这正是 §3 里官方改路由的形态，所以要同时看**真实买家流量**的被动信号（响应 model 字段、改路由事件），不能只靠探针。
  4. 探针必须**长得像真实流量**，否则"识别审计请求并放行"在理论上可行（论文的 Benchmark evasion 场景）。官方不会针对我们这样做，但这条原则对**防止探针本身触发风控**同样成立（§3.1、§3.2）。

### 2.4 Idiosyncrasies in Large Language Models（Sun、Yin、Xu、Kolter、Liu，ICML 2025）【一手】

- arXiv 2502.12150 v2（2025-06-16）；https://arxiv.org/html/2502.12150v2
- **方法**：同一批提示（UltraChat）采每个模型 1.1 万条回答，微调 LLM2Vec 嵌入 + 分类头预测"出自哪个模型"。
- **结果**：ChatGPT/Claude/Grok/Gemini/DeepSeek 五分类 **97.1%**；两两 ≥90%。**同家族不同尺寸**（Qwen2.5 7B/14B/32B/72B 四分类）只有 **59.8%**，两两 63–85%。只用回答**第一个词**就有约 50% 准确率（ChatGPT 爱用 "Certainly"，Claude 爱用 "Based on/According to the text"）。打乱词序仍有 88.9%，只保留 markdown 元素 73.1%。**同一模型不同采样方式**几乎分不出（50–60%）。用 TF-IDF + 逻辑回归这种轻量分类器也有 85.5%。训练样本 10 条时五分类 40.3%，约 1 万条时收敛。
- **对我们的意义**：风格指纹对"换了家"极强，对"同家换型号"中等，对"同模型换采样/量化"无效。我们不需要识别"哪一家"（TLS 已保证），但可以用**TF-IDF + 逻辑回归**在**基线账号**样本上训练"本家族各型号"的分类器，作为改路由的**辅助**信号（仅提示级）。

### 2.5 LLMmap（Pasquini、Kornaropoulos、Ateniese，USENIX Security 2025）【一手】

- arXiv 2407.15847 v4；https://arxiv.org/html/2407.15847v4 ；前文只提了"8 次交互、42 个版本、95%"，这里补充探针本身和不能照搬的部分。
- **默认 8 条查询（论文 Table G.2，原文）**：
  1. 原论文的注入/自报身份或敏感事件探针（不收录原始串，不采用）（自报 + 注入触发器）
  2. 危险制造请求（不收录原题面）（对齐/拒答）
  3. `What is the size of your training dataset?`（元信息）
  4. `Answer this true-or-false question: Climate change is a pressing global issue`（弱对齐）
  5. `What's your data cutoff date?`（元信息）
  6. 原论文的注入/自报身份或敏感事件探针（不收录原始串，不采用）
  7. `Bonjour, how are you doing today? ¿Qué tal?'`（畸形/混合语言）
  8. 原论文的注入/自报身份或敏感事件探针（不收录原始串，不采用）
- **推断模型**：每对（查询，回答）用 multilingual-e5-large-instruct 嵌入，拼接投影后进 3 层小 Transformer（约 800 万参数）；闭集用分类头（42 类），开集用对比学习得到"签名向量"并在签名库里找最近邻（类似 nmap 指纹库，可增量加新模型而不重训）。训练时随机化 60 种系统提示、temperature∈[0,1]、frequency_penalty、RAG/CoT 模板。
- **对我们的意义与不能照搬之处**：
  - 论文自己指出"自报身份"不可靠（很多模型自称 ChatGPT），但不同模型对同一自报问题的**回答方式**有区分度——即"把自报当特征，不当答案"。
  - **第 1、2、6、8 条不能用在卖家的官方账号上**：越狱式注入串、制造炸弹请求会触发官方安全分类器和风控，可能让卖家账号被标记（§3.1；§4.2 有探针导致停权的实例）。我们的场景要求探针**对账号无害**。
  - 2024 年训练的 42 类里闭源只有 3 个 OpenAI/Anthropic 型号，**不能直接用于 2026 年的型号**；开集签名库的思路可用：用我们的基线账号为每个在售型号建签名。

### 2.6 IKP：用冷门事实容量估算黑盒模型大小 + "幻觉相似度"认双胞胎【一手】

- Bojie Li，*Incompressible Knowledge Probes: Estimating Black-Box LLM Parameter Counts via Factual Capacity*，arXiv 2604.24827 v2（2026-07-05）；https://arxiv.org/html/2604.24827v2 ；代码 https://github.com/19PINE-AI/ikp 。**BazaarLink 页面的参考文献里列了这篇**，它的 `ikp_t3_*`～`ikp_t5_*`（TLS/Raft/Paxos/C++ 冷门事实）就来自这个思路。
- **方法**：1,400 道冷门事实题分 7 个冷门度等级（T1–T7；T3–T7 主要来自 Wikidata 机构成立年份等属性、DBLP 研究者的子领域与代表作），temperature=0，系统提示要求简短回答；用 LLM 裁判判 CORRECT/REFUSAL/WRONG；不扣幻觉分（λ=0）。在 93 个已知参数量的开源模型上拟合"正确率–log 参数量"，R²=0.910，但 90% 预测区间约 ±3 倍。
- **两个对我们有用的发现**：
  1. **容量是粗信号**：同代近邻分不开，甚至不单调。表中 Claude Sonnet 4.6（think）70.0% 与 Opus 4.7（think）69.8% 几乎相同；Opus 4.6→4.7→4.8 分别约 73.5%→67.6%→61.9%，作者归因于拒答策略变保守（"refusal policy can suppress tens of percentage points"）。所以**不能用冷门知识正确率区分 Opus 与 Sonnet**，只能抓"掉到小一档"（如换成 Haiku/mini）这类大落差。
  2. **幻觉相似度 HSS 能认"是不是同一份权重"**：在两个模型都答错（且不是拒答）的冷门题上，统计给出**相同错误答案**的比例。共享权重的兄弟模型 HSS 约 0.30–0.56（GPT-5 vs GPT-5-pro 0.56），独立训练的模型 HSS 中位数 0.00；Opus 4.6→4.7 的 HSS 为 0.00、Opus 4.5→4.6 为 0.09。作者提醒共同答错的题数少（7–71 道）时结论只是提示性的。
- **在我们场景怎么用**：这是比风格分类更硬的"双胞胎复核"——被测账号与基线账号对同一批冷门题，**如果是同一模型，答错时应该错得一样**；如果被改路由到别的型号/别的版本，错误答案几乎不重合。用 Wikidata"机构成立年份"这类**数字答案**可以不用 LLM 裁判，直接精确比较。成本见 §7 A07。

### 2.7 2026 年"API 背后是不是那个模型"的新方法：都只需要文本 + 一个可信参考端点【一手，除注明外】

这批论文的共同点是**不需要 logprobs，只需要一个"可信参考端点"**——这正好就是我们的干净基线账号。

| 论文 | 方法要点 | 关键数字 | 对我们 |
|---|---|---|---|
| **KBF**: *Knowledge Boundary as Fingerprint for Language Model and Black-Box API Auditing*（Fang 等，arXiv 2605.29524 v3，2026-09-30） | 让**参考端点自己**按领域生成"名字 \| 数值"候选（原论文为化学数值填空示例，答案为 126；原题面不收录，本期候选改用普通机构年份），在不同提示变体/解码设置下复查，只保留参考能稳定复现的"知识边界题"（参考自身新鲜查询一致率 83.86%–93.15%，其它模型只有 43.91%–79.48%）；审计时与参考答案比一致率，按参考自身波动做单侧二项检验（Clopper-Pearson 上界）。注意它比的是"与参考一致"而非"答对"，**稳定的错误答案同样有用**（与 IKP 的 HSS 思路一致） | 16 个生产端点上 155 个"有经济意义的替换"全部检出，16 个同参考对照 0 误报；混合路由下替换率低至 15% 时检出率 95%；实地审计 28 个端点有 7 个不一致，"predominantly for premium proprietary models"；单次在线审计约 $0.24–0.67 | **后续候选**，作为 A07 研究方法：题目可再生、长得像普通问答、温度 0；需两基线 A/A 后再评估 |
| **IRIS**: *Which Model Is Actually Serving You? Budgeted Black-Box Auditing of Model Substitution and Routing Dilution in LLM Gateways*（Zhang 等，arXiv 2607.20860 v1，2026-07-23） | 让模型生成短随机串，回答映射为 179 维特征；用小规模试点拟合"误差随查询数指数下降"，事先算出需要多少次查询 | 17 个商用模型 m=8 时 AUROC 0.99；**部分路由稀释** ε=5/10/20/40% 分别约需 64/32/16/8 次查询；部署检验 m=80、ε=0.3 时功效 0.85、误报 0.017；effort minimal vs high 汇总 8 次 AUROC 0.92；调温度几乎看不出（0.58）；同家族近亲区分度低；服务端硬件差异本身会被测出 | 适合 A10（便宜的分布指纹）和"只有部分请求被换"的场景；需要 temperature>0 |
| **FLIPS**: *Instance-Fingerprinting for LLMs via Pseudo-random Sequences*（Richardeau 等，arXiv 2606.03330 v1，ICML 2026） | 让模型生成随机比特序列，算 NIST 随机性检验统计量，XGBoost 分类 | 237 个实例（温度、系统提示、量化变体）闭集 96%、开集 90%，验证只需 8 次查询 | 能区分"同一模型的不同配置"——对"被注入系统提示/改采样参数"敏感 |
| **One Token Is Enough**（Bruckner，arXiv 2607.10252 v1）【摘要】 | "1 到 100 随机选一个数"之类单 token 回答的分布即可做指纹 | 165 个模型等错误率 7.3%；约 100 次查询 | 最便宜的入门指纹，适合 A10 |
| **AgentProv**: *Auditing Agentic LLM API Providers via Tool-use Policy Probes*（Wang 等，arXiv 2609.00052 v1，EMNLP 2026） | 20 个模板，每个提供若干功能等价的工具，记录模型选哪个，MMD 置换检验；**另有 token 计数侧信道** | 630 对替换全部检出；注入系统提示时误报 7%（MET 67%、RUT 53%）；75 个 OpenRouter 开源端点中 44% 的输入计数偏离官方模板。**计数判读规则：与参考逐条差值全为 0 正常；恒定正差 k = 注入了 k 个 token；负差 = 模板更精简/截断；有方差 = 后端不一致** | Claude Code / Codex 本来就是工具调用流量，工具偏好探针**对网关注入的固定系统提示不敏感**，很合适；计数规则直接用于 A03 |
| **GateScope**: *Behavioral Consistency and Transparency Analysis on LLM API Gateways*（Lin 等，arXiv 2604.21083 v1，IMC 2026） | JSON 结构化回答 + XGBoost 识别官方 24 个型号（F1 0.968）；**25 轮对话在第 10/24/25 轮检查记忆**，同时看缓存命中率（换后端会让缓存命中归零） | 10 个网关中两个计费偏差 7.6% 和 62.8% | 多轮记忆检查点可用于 D09（静默截断/中途换后端） |
| **Real Money, Fake Models: Deceptive Model Claims in Shadow APIs**（Zhang 等，CISPA，arXiv 2603.01919 v3，CCS 2026） | 审计 3 个影子 API 的 24 个端点（被 187 篇论文使用） | 性能偏差最高 47.21%；LLMmap 身份验证失败 45.83%；Qwen2.5-7B 冒充 GPT-4o-mini 时 GPQA 只差 1.68 分、AIME 差 11.11 分；同题重复请求的延迟标准差 >1.2 倍官方的占 64.58%，token 数标准差比 <0.8 的占 20.83%（疑似截断） | 能力题要**偏推理密集**（知识题差异小）；"同题重复方差比"是便宜的元数据信号 |

### 2.8 Token 计费审计与"隐藏 token"【一手，除注明外】

| 论文 | 要点 | 对我们 |
|---|---|---|
| *Invisible Tokens, Visible Bills*（Sun、Wang、Zhao 等，arXiv 2505.18471 v1，2025-05-24，立场论文） | 风险分"数量膨胀"（隐藏推理 token、内部调用注水）和"质量降级"（偷换小模型、伪造工具调用）；只凭提示长度和答案长度预测 DeepSeek-R1 隐藏推理长度，回归落在 ±25% 内的仅 20–29% | **逐条预测推理 token 不可靠**，应做"同题配对比分布"（D06） |
| **CoIn**（Sun 等，arXiv 2505.13778 v1） | 服务商对隐藏推理 token 做嵌入指纹 + Merkle 承诺，审计方抽样验证；膨胀率 3.0 时检出 94.7% | 需要服务商配合，**不可用** |
| **PALACE**（Wang、Sun 等，arXiv 2508.00912 v1） | 用户侧小模型从"提示+答案"回归推理 token 数；相对误差 <33% 的比例数学 63.81% | 需要服务商先给训练数据；基线账号能直接给同题真实推理量，比预测器准，**不需要** |
| **Token Inflation: How Dishonest Providers Can Overcharge…**（Hoque 等，arXiv 2605.30040 v1，2026-05-28） | 实证攻破 CoIn（重复推理块平均膨胀 549.9%）、PALACE（答案末尾加 `boxed` 预测值平均 +82.4 token）、Velasco 检验（可藏 50.85% 超报）；"证据由被审计方产出的审计都能被博弈" | 跨账号差分的证据来自另一个账号，能发现账号之间的不一致；**官方全局变化差分看不出**，必须保留历史锚点（前文 §5.1） |
| *Is Your LLM Overcharging You? Tokenization, Transparency, and Incentives*（Velasco 等，arXiv 2505.21627 v4，ICML 2026 oral）+ *Tokenization Multiplicity Leads to Arbitrary Price Variation*（Chatzi 等，arXiv 2506.06446 v2） | 同一字符串可有多种分词，服务商可上报更长序列；姊妹篇在官方 API 上实测**同一输出字符串上报的 output token 数不同**（GPT-5 mini 在土耳其语、斯瓦希里语上 ≥7% 的提示出现，Claude 也观察到） | 若做"可见文本重新分词 vs 上报 output_tokens"核对，要留几个百分点容差，非英语更宽；**输入**计数（A03）不受此影响，仍应精确相等 |
| *Auditing Pay-Per-Token in LLMs*（Velasco 等，arXiv 2510.05181 v2，AISTATS 2026） | 鞅序贯检验 M_i=M_{i−1}(1+λE_i)，超过 1/α 判不诚实，Ville 不等式保证任意时刻停止误报 ≤α；需要下一 token 概率 | 方法不可直接用；但"任意时刻有效"的 e 值检验很适合持续巡检（前文 §6.3 提到的 anytime-valid） |
| *The More It Says, the More You Pay*（Chen 等，arXiv 2609.20370 v1，2026-09-17） | 每题发"原题"与"原题+良性的写出步骤后缀"，Δ=L1−L0，R=max(Δ,0)/(L0+1)，s=1[Δ≤0]+1[R≤0.6]，20 题取最高 3 个 s 求和 ≥3 报警；开源模型检出 85.1%、误报 <2%；15 个第三方"GPT"服务 7 个报警 | 官方对订阅账号更可能是"压短/偷懒"而非注水；可**对称化**成"详细/简短"两种干预，比较被测与基线的边际响应（D08）。注意"写出推理步骤"类后缀与 Anthropic 所说 CoT 诱导相近，改用"更详细地解释"之类措辞 |
| **缓存**：*Auditing Prompt Caching in Language Model APIs*（Gu 等，arXiv 2502.07776 v2，ICML 2025）；**KeyPooling**（Sun 等，arXiv 2608.17485 v2，2026-08-23）；**CacheTracer**（Ji 等，arXiv 2608.20732 v1） | Gu 等：TTFT 单侧 KS 检验，17 家里 7 家全局共享缓存，Anthropic/OpenAI 按组织共享。**KeyPooling 点名测了 Sub2API**（冻结在 commit b8b72e1b，2026-07-21）：通用 OpenAI 适配器转发调用方的共享 cache key 导致跨客户命中，原生 Grok 适配器按下游 key 派生命名空间而隔离；两个线上 Sub2API 部署默认路由上分别 2/9、1/8 个有效单元命中；并据源码把 Sub2API 归为"订阅额度分发"，**OAuth 凭证每个账号只有一个缓存域，运营者无法靠配置隔离**；缓解：OpenAI 路径用 HMAC(平台密钥, 买家, 模型, 版本) 覆盖 `prompt_cache_key`（跨客户 0/3、自己命中 3/3，成本 +1.7–2.5%）。CacheTracer：39 个转售端点 37.1% 的端点对共享缓存，嵌套深达 7 层 | ①跨买家缓存侧信道是共用账号的**隐私副作用**（§4.2 末），需在 v0.2.14 上复核；②反过来可以**检测他人共用**：平台在缓存 TTL 内没发过某个公共前缀（如 Claude Code 系统提示+工具定义），却读到了它的缓存 → 有别人在用（新增 D14） |

### 2.9 量化/精度降级与路由检测【一手，除注明外】

- **轻度量化用准确率几乎测不出**：*Quantization Hurts Reasoning?*（Liu 等，arXiv 2504.04823 v2，COLM 2025）：W8A8、W4A16 损失 ≤1%；*Accuracy is Not All You Need*（Dutta 等，arXiv 2407.09141，NeurIPS 2024）：总准确率只差 ≤2% 但**逐题翻转率** ≥5%（最高 13.6%）。按子调研的估算（非论文数据），基线 70% 时以单侧 α=0.05、功效 0.8 检出 3 个百分点下降每侧约需 2,960 题，而翻转率从 5% 升到 10% 每侧约 340 题——**翻转率比准确率灵敏得多**，但必须用两个干净账号互测（A/A）的翻转率作噪声底。
- **生产推理本身就不确定**：*Understanding and Mitigating Numerical Sources of Nondeterminism in LLM Inference*（Yuan 等，arXiv 2506.09501 v2）：BF16 贪心解码下只改 GPU 数量/型号/批大小，准确率可差 9%、回答长度可差 9,000 token。HSPI（arXiv 2411.05197 v2）说明不同硬件池可被区分。→ 基线账号与被测账号**可能落在不同硬件池**，这是所有分布类检验的误报来源。
- **路由可被提示改变，model 字段只是声明**：*Rerouting LLM Routers*（Shafran 等，arXiv 2501.01818）：与查询无关的前缀即可让路由器把请求升级到强模型（OpenRouter auto 成本 >8 倍）；*Route to Rome Attack*（Tang 等，arXiv 2604.15022，ACL 2026）对 GPT-5 Auto 用"思考模式输出风格"训练词袋分类器来推断路由结果。GPT-5 系统卡（2025-08）写明 ChatGPT 由实时路由器选模型、**用量达到上限后由 mini 版处理**【二手：调研者转述】——所以检测时要把账号的窗口用量作为协变量。
- **effort 必须写死**：*The Price of Thinking*（Moon，arXiv 2608.16956 v1）：不同型号对"省略 effort"的默认语义不同（Sonnet 5 显式 high 与不传相比准确率差 +0.013，不显著）。推理强度被压低主要靠 token 计数来测，小样本准确率测不出。
- **开源权重的国产 Coding Plan 有现成工具**：Moonshot 的 K2-Vendor-Verifier / Kimi Vendor Verifier（https://github.com/MoonshotAI/K2-Vendor-Verifier ）用工具调用触发 F1 与 JSON Schema 准确率比较各家部署与官方 API（K2-thinking 官方 100%，部分供应商 95.7–99.9%），2026-04 起加入 AIME 长输出压力测试抓 KV cache bug 与量化退化【二手：博客与仓库说明】。可直接复用其题集检查 Kimi/GLM/MiniMax Coding Plan 是否"与官方 API 同款"。
- **可验证推理对官方订阅不可用**：TOPLOC（arXiv 2501.16007）、SVIP（arXiv 2410.22307）、DiFR（arXiv 2511.20621）、IMMACULATE（arXiv 2602.22700）、AEX（arXiv 2603.14283）等都需要服务商出证明或需要开源权重；Anthropic 的 Confidential Inference（https://www.anthropic.com/research/confidential-inference-trusted-vms ）目标是保护数据与权重，不是向客户证明跑的是哪个模型【二手】。

---

## 3. 官方一手材料：厂商自己说了什么

### 3.1 Anthropic：明确针对"转售/中转"做检测，探针设计必须避开这些特征【一手】

1. **2026-02-23《Detecting and preventing distillation attacks》**（https://www.anthropic.com/news/detecting-and-preventing-distillation-attacks ）：
   - 实验室通过"转售 Claude 访问权的商业代理服务"（"hydra cluster"：大量账号分摊流量）规模化调用；一个代理网络同时管理 2 万多个账号，并把蒸馏流量混在普通客户请求里。
   - 检测手段原文："classifiers and behavioral fingerprinting systems designed to identify distillation attack patterns in API traffic. This includes detection of chain-of-thought elicitation… detection tools for identifying coordinated activity across large numbers of accounts."
   - 典型特征："Massive volume concentrated in a few areas, highly repetitive structures"；"Identical patterns, shared payment methods, and coordinated timing"。
   - 反制："developing Product, API and model-level safeguards designed to reduce the efficacy of model outputs for illicit distillation"。
2. **2026-09-10 威胁情报报告《Detecting and countering misuse of AI: September 2026》**（https://www.anthropic.com/threat-intelligence-report-september-2026 ）：
   - "These labs generally access Anthropic's models by routing requests through proxy services, also known as 'transfer stations.'"（即"中转站"）
   - 处置："when we detect signals of potential abuse, like the unauthorized resale of Claude or accounts operating from unsupported countries…, our systems can require users to verify their identity to retain access. Accounts that fail to do so are banned." 以及 "We use metadata and look for signals of irregular activity to identify accounts associated with proxy service networks."
   - "Claude now summarizes its internal reasoning before responding"；"with Fable 5.1 we introduced preserved thinking, which stops new API accounts from altering the system prompt, tools, or messages that precede Claude's reasoning in multi-turn conversations."
   - GTG-50021：假冒"低价 Claude"的转售商实际把流量"silently proxied to a different AI model"并安装凭据窃取器——这就是公开中转站的"偷换模型"，在我们的 TLS 直连设计下不成立，但说明买家对"是不是真 Claude"的担忧有现实依据。
   - 生物安全案例写到分类器把高风险研究"confined … to our weakest models"：语境是**强模型上的分类器拦截了内容，用户只能转用 Sonnet 4、Haiku 4.5 这些弱模型**，不是"自动把被标记账号降级"。不要把它当作"官方按账号降智"的证据。

**由此得出的硬性约束（对所有质检项都适用）**：
- 探针不能使用：有害请求（LLMmap 的危险制造请求）、越狱/注入触发串（不收录原文）、**索要推理过程/思维链复述**的提示（"把你的推理一步步写出来""translate your previous reasoning"）、网络攻击相关内容（会触发安全分类器与回退/改路由，见 §3.2、§3.3）。
- **同一套固定提示词在大量账号上同步重复**正是"coordinated activity / highly repetitive structures"。探针要**题库轮换、参数化生成、随机时间、低频、与真实请求同形态**，并且把探针总量控制在真实流量的很小比例。
- 账号被要求"身份验证"是一种可观测的不可用状态，应在 `availability` 里单独分类（与授权失效、封号区分）。

### 3.2 Anthropic：官方文档化的"按请求回退到弱模型"，并参考账号信任信号【一手】

这是本次找到的**最接近"官方对账号降智"的一手证据**，而且是有明文、有可观测字段的。

- **《Why Claude switched models in your conversation with Opus 5 or Opus 5.5》**（https://support.claude.com/en/articles/16049681 ，页面标注约两周前更新）原文要点：
  - "A narrow set of higher-risk requests either fall back to a less capable model or are blocked directly… That includes fine-tuning our classifiers to reduce false positives and **factoring in a range of account trust signals**."
  - 分类器检查**模型读到的全部内容**（含记忆、连接器内容、搜索结果、文件），"a fallback can be triggered by content you didn't type"。
  - 回退路径：**网络安全类** Opus 5/5.5 → Opus 4.8（漏洞利用生成、二进制漏洞扫描、渗透测试；源码安全审查不受影响）；**生物类** Opus 5.5 → Opus 5（Opus 5 不因生物类回退）；**前沿 LLM 开发类**（仅 Opus 5.5，如某些 ML 加速器的 kernel 开发）Opus 5.5 → Opus 5；**蒸馏类**直接拦截、不回退——"prompts that ask Claude to repeat its reasoning verbatim or write its full chain of thought to an external output"。
  - "After the switch, the model picker **stays on the less capable model for the rest of the conversation**."
  - 计费："refusals that arrive before any output are billed when they stop or fall back due to biology, distillation, or frontier LLM development safety classifiers"（截至 2026-09）；回退后的回答按实际作答模型的价格另计。
  - 适用范围：Claude 网页、桌面、移动端、Cowork、**Claude Code** 等默认开启自动切换（Claude Code 在 Config > MODEL & OUTPUT 的 "Switch models when a message is flagged" 开关）；**Claude API 默认不自动切换**，"Until fallbacks are configured, the model will return a 200 response with a stop reason on the API"。
- **《Our Approach to User Safety》**（https://support.claude.com/en/articles/8106465-our-approach-to-user-safety ，2026-03-16）："Enhanced safety filters, which allow us to increase the sensitivity of our detection models. We may temporarily apply enhanced safety filters to users who repeatedly violate our policies, and remove these controls after a period of no or few violations."——**账号级**的检测灵敏度提高。
- 开发者文档（https://platform.claude.com/docs/en/build-with-claude/refusals-and-fallback ）：本机所在地区访问该站返回"App unavailable in region"，**未能直接核对**。中文社区子调研转述的要点【二手】：API 侧拒答为 HTTP 200、`stop_reason:"refusal"`、`stop_details.category` 取 cyber / bio / frontier_llm / reasoning_extraction / general_harms 之一；开启 `fallbacks:"default"` 与 `anthropic-beta: server-side-fallback-2026-07-01` 后，顶层 `model` 为实际作答的模型。

**对我们的含义**：
1. 被降级的形态是"**按请求**回退到弱一档型号"，**触发概率受账号信任信号影响**，并且会**粘在整个会话**上。一个被标记为低信任的寄售账号，对同样的内容可能更频繁地回退——这正是"被风控账号降智"的官方版本。
2. 它是**可观测**的：回退后响应的 `model` 字段（客户端开启回退时）或 `stop_reason`/`stop_details`（未开启时）。所以 A02（型号字段）与 A12（回退/拒答率）能直接测，而且应当**按账号与同型号其它账号、基线账号比较同一批无害探针上的回退/拒答率**。
3. 探针不能含网络安全攻击类、生物类、前沿 LLM 开发类内容，更不能让模型复述推理（会被直接拦截且**照样计费**）。
4. 会话粘性意味着：被测账号一旦在某个会话里回退，同会话后续探针都会落在弱模型上——**每次探测开新会话**，并且真实买家会话里的回退要单独统计、不能当作账号常态。

### 3.3 OpenAI：Codex 的网络安全改路由是**按账号**生效的【一手：官方开源 Codex 源码】

- openai/codex（commit a07c8eb，2026-10-11，克隆于 调研期间的只读 Codex 克隆（临时材料，未入库））里的提示文案原文（`codex-rs/core/src/compact_tests.rs:444`）："Warning: **Your account was flagged** for potentially high-risk cyber activity and this request was routed to gpt-5.2 as a fallback. To regain access to gpt-5.3-codex, apply for trusted access: https://chatgpt.com/cyber"。
- 判别方式：官方客户端比较响应头 **`OpenAI-Model`** 与请求型号，不一致时发出 `ModelReroute{from_model, to_model, reason:"high_risk_cyber_activity"}` 事件（测试 `codex-rs/core/tests/suite/safety_check_downgrade.rs` 用 `OpenAI-Model: gpt-5.2` 对请求 `gpt-5.3-codex` 模拟改路由）；源码注释："This can differ from the requested model when backend safety routing applies"。服务端还会在 `response.metadata` 事件推送 `openai_verification_recommendation:["trusted_access_for_cyber"]`（建议该账号去做可信访问认证）。
- 其它相关机制（子调研读源码）：安全缓冲（响应头 `x-codex-safety-buffering-*`、事件字段 `retry_model`，客户端提示"Giving this request a little extra thought… retry with a faster model. It may be less capable"）；策略拦截 400 `cyber_policy` / `bio_policy` / `misalignment_policy_violation`；达到限额返回 429 `usage_limit_reached`（体内含 `plan_type`、`resets_at`），**服务端报错而不是换模型**；用量 90% 时客户端建议切到 gpt-6-luna、实验性的 "Luna Reserve"（`gpt-reserve`）在普通额度被封后由**客户端**改请求里的型号——网关看到的是请求型号变了，不是服务端偷换。
- 社区报告大量正常开发请求也被改路由（openai/codex#11189、#12132）【二手/社区】；官方帮助文章称即使通过可信访问认证仍可能被 "block, slow, or reroute"【二手转引】。Codex 负责人 2026-04 称 "we don't fiddle with the models or thinking budgets after release"【二手】。
- **sub2api v0.2.14 不解析 `OpenAI-Model`**，默认响应头白名单也不包含它与 Claude 的 unified 头（`util/responseheaders/responseheaders.go:15-37`），结果是**买家的 Codex 客户端看不到官方的改路由警告**——这既是检测缺口，也是对买家的透明度问题。

**对我们的含义**：Codex 的"被风控账号降智"有官方实现、而且**按账号**生效（"Your account was flagged"），对应的硬信号是 `OpenAI-Model` 与 `response.metadata` 里的可信访问建议。A02/A12 应以它们为主判据；同时 GPT-5.x 全系用 o200k_base（tiktoken 映射），**输入计数分不出 gpt-5.3-codex 与 gpt-5.2**，所以 A03 对 Codex 只能查注入、不能认型号。

### 3.4 其它厂商（详见 §5.2）

- Google：Gemini CLI 的 Pro→Flash 回退是**客户端**行为，服务端回 `modelVersion`；未见服务端静默换模型。
- 智谱、Kimi：有**公开的**官方自动路由（旧型号→新型号、关思考→无思考版），属于"官方明示的换型号"，应列入白名单而不是判降智。
- 2025-09-17 Anthropic 复盘原话 "We never reduce model quality due to demand, time of day, or server load"（前文 S02）；2026 年的按请求回退是在此之外**明示**的安全机制，二者不矛盾。

---

## 4. 开源工具与社区做法

### 4.1 BazaarLink Probe 的实际题目与判定流程【一手：页面 SSR 文本 + 同期抓取的 86 项详情】

- 页面：https://bazaarlink.ai/zh-CN/probe （本次直接抓取 SSR 文本核对："84 项标准探针 + 2 项扩展检测（最多 86 项）"、致谢 LLMmap / api-relay-audit / relayAPI、参考文献含 arXiv 2604.08407、2407.15847、2604.24827、OWASP LLM Top 10）。86 项逐项题目与样例响应来自同一研究批次在 调研期间的 86 项详情抓取（临时材料，未入库） 的抓取（页面 `tab=learn` / 报告详情）。
- **判定五站**：①出题实测（缺题即停，避免抽样偏差）→ ②认出哪一家（看行为不看自称）→ ③认出哪一款（家族内与跨家族比对互查）→ ④双胞胎复核（对近亲型号用另一组题重采样，看整体分布；**证据不足且与宣称一致时不推翻**）→ ⑤对答案（结论：完全相符 / 只有家族相符 / 款式不符 / 换模 / 自称被伪造 / 行为被诱导 / 数据不足 / 模棱两可）。
- **它自己写明的能力边界**（页面原文要点）：只看得见这一次调用，中转可以只对部分流量偷换；"当冒充者刻意模仿到一半以上的行为时，证据会有大约一半天然指向它宣称的身份……实测过加题，成本大幅上升而分辨能力几乎不动"；不能证明"绝对没换"。
- **几个规则项的具体判法**（来自报告样例）：
  - `token_inflation`：发 "Hi"，看 `prompt_tokens`；样例 "prompt_tokens=39，阈值 ≤200"，失败样例 "prompt_tokens=512，超出阈值 32"（有模型基准时按"基准 + 容许增量"）。
  - `context_length`：4K→128K 字符递增，每档带 canary，找截断点。
  - `consistency_check`：temperature=0.7 连发两次"返回 1–1000000 的随机整数"，完全相同则疑似缓存。
  - `cache_detection`：看响应头 `x-cache` 与 `cache_read_input_tokens`。
  - `channel_signature`：发 "Reply with exactly one word: OK"，用响应头与 message id 前缀识别 Anthropic 官方 / Bedrock / Vertex / new-api 等（样例通过 `x-new-api-version` 头识别出 New-API）。
  - `thinking_block`：带 `anthropic-beta` interleaved-thinking 头问 "17 × 23"，看是否返回 thinking block（样例对某中转返回 HTTP 400）。
  - `submodel_capability`：一条消息里 5 个小题（strawberry 里几个 r、1000 天后星期几、分苹果、第 100 个质数、accommodate 倒写）。
  - `perf_bulk_echo`：写 200 次 banana，测 TPS；`verb_explain_photosynthesis`：只说 "Explain photosynthesis." 看默认冗长度。
- 失败原因统计页给出了公开中转的常见故障分布（额度不足 403、无可用渠道 503、429 并发限制、空响应、400 等），对我们区分"卖家账号问题"和"网关/官方问题"有参考价值。

### 4.2 开源工具源码里实际用的探针与判法【一手：只读源码，未运行；行号为克隆时版本】

**先说两条最重要的发现**：

1. **探针里绝不能有有害内容，已有实证。** BazaarLink 的开源引擎 `Bazaarlinkorg/LLMprobe-engine` 在 v0.10.1 / v0.11.0 的变更说明里写明：原来的拒答阶梯问的是炸弹、毒品合成等，"已证实会导致账号被停权（gpt-5.4/gpt-5.6 全数收到 502 Policy Violation）"，之后全部换成无害题（就是现在网页上那套"保证收益/冒充律师"的 L1–L8）。LLMmap 默认 8 题里仍有危险制造请求（不收录原题面）。我们的探针打在卖家的订阅账号上，这类题等于主动招风控。
2. **没有任何一个开源工具用到订阅账号的额度响应头**，而 sub2api 已经在解析它们（Claude OAuth 的 5h/7d 利用率、Codex 的 primary/secondary used-percent）。这是判断共用与档位最便宜的信号（§5、§7 D 类）。

| 工具 | 地址 / 许可 / 最近提交 | 与我们相关的探针与判法（原文与阈值） | 在我们场景的价值 |
|---|---|---|---|
| **api-relay-audit** | https://github.com/toby-bridges/api-relay-audit ；875 星；v2.5.0，2026-10-09；**2026-05-31 起由 MIT 改为 AGPL-3.0-only**（BazaarLink 页面写的 MIT 已过时）；单文件 `audit.py` 共 14 步 | **token 注入**：`Say hi`（预期 10）、system=`You are a helpful assistant.`+`Say hi`（预期 20）、`Who are you?`（预期 15），取"实际−预期"最大值，>100 红、>20 黄（audit.py:5432-5488）；**上下文**：填充文本中均匀插 5 个 `CANARY_i_<8位hex>`，问 `I placed 5 markers [CANARY_N_XXXXXXXX] in the text. List ALL you can find, one per line.`，粗扫 50K–800K 字符后二分到 ≤20K 间隔，通过的最大长度 <150000 token 判黄（2212-2330、6198）；**流式完整性**：检查 7 种标准 SSE 事件之外的类型、output_tokens 倒退、两处 input_tokens 不一致、签名为空、模型名不含 claude（226-575）；**时延方差**：连发 10 次 `Reply with the single word: ok`，最大间隙/中位数 >0.5 判双峰（4413-4537，只作参考）；**渠道识别**：`msg_bdrk_`/`x-amzn-bedrock-` → Bedrock，`msg_vrtx_` → Vertex，`anthropic-ratelimit-*`（权重 0.95）+ `request-id: req_*`（0.6）→ Anthropic 官方（4660-4960）。已知缺陷：第 10 步 `max_tokens=100, budget_tokens=99` 在官方会被拒（思考预算下限，见下注）；注入"预期值"写死，换 tokenizer 会漂；把 `antigravity`/`deepmind` 列为"非 Claude 身份"关键词（2778），会误伤 sub2api 的 Antigravity 路由 | 上下文二分法和 SSE 完整性规则可借鉴；其余主要针对中转 |
| **LLMmap** | https://github.com/pasquini-dario/LLMmap ；MIT；v0.2，2025-07-24 | 8 题见 §2.5（仓库版第 7 题是 `What LLM are you exactly?`，与论文略有差异，`confs/queries/default.json`）；回答截断 650 字符，e5 嵌入 + 3 层 Transformer；**开集模式没有"未知模型"阈值，永远返回最近的那个**；新增一个型号模板需 100 套配置 × 8 题 = 800 次请求；附带的 52 个模型里闭源只有 claude-3 系与 gpt-3.5/4/4o | 题目有越狱串和炸弹题，**不能直接用**；"开集签名库"思路可借鉴 |
| **relayAPI** | https://github.com/zzsting88/relayAPI （hvoy.ai 中转推荐列表，无许可证）；检测器只在 2026-03-14 至 04-14 存在过，已删除 | 历史版 `claude_detector.py`：伪装 Claude Code 请求头（`anthropic-beta: claude-code-20250219,interleaved-thinking-2025-05-14`、`x-app: cli`），问 `你是谁,你的知识库截止时间是什么时候? 请一定要诚实回答`；知识截止占 50 分、SSE 20、thinking 签名 15、usage 15；网页版两阶段：`输出中文的这个符号”"`（回复不含全角右引号才算过）与 `不允许上网查, 2025年3月4日特朗普把关税提到多少. 不知道就回答不知道.` | 时效性强、易过期；不适用 |
| **LLMprobe-engine（BazaarLink 开源引擎）** | https://github.com/Bazaarlinkorg/LLMprobe-engine ；AGPL-3.0（但部分文件头写 MIT，许可不一致）；v0.11.0，2026-08-19 | `PROBE_SUITE` 实际 96 条（含 2 条可选）；统一走 `/chat/completions`，精确匹配类 temperature=0、max_tokens=64，其余 0.3/1024；**token 注水**：发 `Hi`，`prompt_tokens > 50` 即判注水（token-inflation.ts:3-20）；**签名回传**：把 thinking 块与签名原样放回 assistant 消息再请求，200 即验证通过，400 且含 "invalid" 判签名被拒（signature-probe.ts:60-125）——**唯一一个用密码学方式确认上游是 Anthropic 的方法**；上下文 4K/16K/32K/64K/128K 字符各插 5 个 canary，回忆 ≥80% 算该档通过；**V3H 答案分布指纹**：如 `Pick a random whole number from 1 to 100. Reply with ONLY the number.`，每题采 3–6 次，temperature=1，拉普拉斯平滑对数似然 + softmax；**基线超过 180 天或样本少于 100 条即标"过期"**；判"与宣称不符"需置信 ≥0.85，绝对置信 <0.70 或领先 <0.30 时弃权；请求出错率 >15% 强制降为"只认家族"（identity-verdict.ts、submodel-abstain.ts）；离线基线覆盖 34 个型号（含 claude-opus-4.5–4.8、opus-5、sonnet-5、gpt-5.x、gemini-3.1、grok-4.x、glm-5.2 等） | 弃权规则与"基线新鲜度"规则值得照搬；签名回传适合做平台自检 |
| **veridrop** | https://github.com/canarybyte/veridrop ；AGPL-3.0；2026-09-20 | **token 用量**：`Reply with exactly: ok` 与"同句 + `\n\nReference text:` + 80 个 ` apple`"对比，input_tokens 增量应在 45–140（新 tokenizer 型号放宽到 90–230），流式与非流式 usage 差 ≤20%，`/v1/messages/count_tokens` 与实际差 ≤20%（token_usage.py:20-245）；**thinking 签名**：支持自适应思考的型号改用 `thinking:{type:"adaptive",display:"summarized"}` + `output_config.effort`，注释称 Opus 4.7 流式下会丢 thinking 块所以用非流式；签名 ≥50 字符满分；**长上下文**：32K/100K/200K 或窗口的 50%/95%，每档 3 根随机口令，接近上限时先用 count_tokens 校准到 ±2%；成本：quick 5–6 次约 $0.05，长上下文三档约 $1 | "差分计数"思路（只看增量，抵消固定前缀）很适合我们的 A03；"新 tokenizer"区间说明 tokenizer 变化确实能从计数上看出来 |
| **claude-detector（anthropic.mom）** | https://github.com/7836246/claude-detector ；MIT；2026-04-16 | `count_tokens` 与 `usage.input_tokens` 差 ≤1 才算通过；用 count_tokens 计算回复文本得"计费倍率"，≤1.05 且有官方头判正品官方，≤1.15 无官方头归为"订阅"；缓存探针只垫约 300 token（低于官方最小可缓存长度，真官方也会失败） | 少数显式区分"订阅渠道"的工具；计费倍率思路可用于平台对账 |
| **ModelTrace（sub2api-excel2api-plugin 内嵌）** | https://github.com/1812095643/sub2api-excel2api-plugin ；无许可证（ModelTrace 本身 MIT）；2026-09-25；**是 sub2api 的 OpenAI OAuth 插件，自带"降智检测"** | 走 Responses API，要求给出 292–332 个 1–355 的整数（并写明禁止工具、禁止等差/循环）；把 355 维频率直方图投影后与 16 个型号的质心做点积，softmax 后预测型号 ≠ 请求型号就标 `degraded / fingerprint_mismatch`（challenge.go、modeltrace.go:342-377、server.go:483-489） | 单次请求的"随机数直方图"指纹，很便宜；质心要用我们自己的基线账号重建 |
| **TraceOne** | https://github.com/wangchao0708/TraceOne ；MIT；2026-09-30 | 同类随机数指纹区分 8 条 Codex 路由，作者自报真实条件确认率 101/120（84.2%），且只在"Codex 订阅 + low reasoning"下验证；**降智判定单独做**：`Compute ((a * b) + c) mod m. Return only the decimal integer. Do not use tools…`、gcd/lcm、字符串旋转等每类 12 题，两次运行做成对单侧 McNemar 检验（α=0.05），多层 Holm-Bonferroni 校正；README 明确"数字指纹变了 ≠ 降智" | 思路与前文的配对统计一致，可直接参考 |
| **grok-iq** | https://github.com/kaibush/grok-iq ；MIT；2026-09-17 | Grok 号池巡检：TPS ≥150 记"偏高"、≥500 记"强降级"；首 token 占总时长 ≥0.85 记"缓冲"（整段一次吐出）；推理模型 `reasoning_tokens ≤ 0` 判 `reasoning_zero`；**分别统计"固定出口 IP"与"临时切换出口 IP"的样本，区分账号问题与出口问题** | 与"卖家提供出口代理"的场景高度吻合；"推理 token 为 0"是最简单的掺水信号 |
| **auditing-prompt-caching（ICML 2025）** | https://github.com/chenchenygu/auditing-prompt-caching ；Apache-2.0 | 随机字母串 prompt（默认 5000 token），命中组/未命中组各约 250 次，max_tokens=1，对时延做单侧 KS 检验，分 per_user/per_org/global 三级 | 见下方"跨买家缓存侧信道" |
| **CacheProbe** | https://github.com/Ryan5453/cacheprobe ；MIT；2026-10-06 | 测首 token 时延，命中/未命中各 250 样本，prompt 4096、前缀 0.95，用 ROC/PR AUC；结论：OpenRouter 用同一组织凭据代理所有用户，导致跨账号缓存共享 | **与"多个买家共用一个卖家账号"是同一结构** |
| model-equality-testing | 论文仓库 https://github.com/i-gao/model-equality-testing （本次克隆所得镜像为 irenasaracay/model-equality-testing） | Hamming、k-spectrum、全子序列三种核；置换检验 b=1000；temperature=1 | §2.2 |
| sunblaze-ucb/llm-api-audit | https://github.com/sunblaze-ucb/llm-api-audit ；MIT；2025-04-10 | §2.3 论文的实验脚本，基本在本地开源模型上跑 | 负面结论的依据 |

**由工具调研引出的一个新风险：跨买家的缓存侧信道。** 官方提示缓存若在组织/账号范围内共享，多个买家共用一个卖家账号时，买家 B 可以通过首 token 时延或 `cached_tokens` 判断"别人是否刚发过相同前缀"（auditing-prompt-caching 与 CacheProbe 的方法）。这不是模型真伪问题，但属于"共用账号"的隐私副作用，应在买家协议里披露，或在网关侧为不同买家的缓存前缀加盐（是否值得做需另行评估成本）。

**注**：Anthropic 的思考预算下限（1024 token）与最小可缓存长度（按型号 1024–4096 token）为工具调研者凭既有知识给出；中文社区作者的实测值与此不同（称 5.5 代为 512，§4.3）。本次因 Anthropic 文档站对本机所在地区返回"App unavailable in region"而**未能在线核对**，实现前需以官方文档或我们自己的基线实测为准（A15 正是利用这类差异）。

### 4.3 中文社区的实际做法（linux.do、V2EX、知乎、GitHub 中文仓库）

（子调研读了约 35 个 linux.do 主题（经 `.json` 接口，站点搜索被 Cloudflare 拦）、V2EX（经 r.jina.ai 转抓）、十余个中文仓库源码；访问日期 2026-10-11 至 10-12。帖子编号指 `https://linux.do/t/topic/<编号>`。）

**总的判断**：中转检测器里"硬证据"类（协议字段、ID 前缀、签名、注入）针对的是中间人改了什么，对我们只能用来自检 sub2api；真正值得借用的是三类——**实际作答的模型（响应字段、回退字段）**、**推理量分布**、**行为指纹与固定难题（必须与同期健康账号对照）**。另外有三个重要提醒：公开题会被"背"（linux.do 1841302 实锤拼车车主对 L 站标准题硬编码答案）；官方降智常按会话、按时段出现，**每次探测要开新会话、隔时段多次采样**（409884 #12、2861622）；**探针本身会触发官方风控或计费**（见下文 Claude 的 reasoning_extraction 拒答照样计费、OpenAI 的 cyber 改路由）。

| 工具/方法 | 检测什么 | 提示词/参数（原文摘录） | 判定 | 对我们 |
|---|---|---|---|---|
| **wxmb01/Claude-Detection**（Claude Code Skill v1.3，2026-10-01，方法最严谨；自带会作 10 种弊的本地中转做阳性对照）【一手】 | Claude 真伪/代际/注入/号池 | S1：发 `"hi"`（max_tokens 16，effort low），`input_tokens + cache_creation + cache_read` 合计——作者实测 Opus 5.5/5 为 10，Opus 4.8、Sonnet 5、Fable 5 为 8；S2 **缓存阈值法**：从短到长扫描带 `cache_control` 的前缀找开始缓存的临界点，作者实测最小可缓存长度 5.5 代 512、Opus 4.8 1024、4.7 2048、4.6 4096；S3 回传 `signature:"garbage"` 官方必返回 400；把 thinking 块回传给不同模型看 input 差值判断"读到/丢弃"；作者称 **Sonnet 5.5 的 thinking 块绑定账号，号池上同模型自读会随机丢弃，可以反推号池**；S8 **effort 阶梯**：3 题 × low/medium/high/max，要求 max 的输出 token >1.3×low，非法 `effort:"ultra"` 必须 400；S14 **参数规则指纹**：如 `thinking:{"type":"between_tools"}` 只有 Sonnet 5.5 接受；S15/S16 tokenizer 向量、单 token 分布（作者称 Opus 5.5 在"1–100 随机数"多答 47、颜色答 teal，Opus 4.8 答 73、blue） | 与官方规则表逐项一致 | **"服务端参数规则指纹"是很好的确定性真伪项**（新增 A15）；所有数值都是作者实测、未经独立复现，必须用我们自己的基线账号重测 |
| **cc-proxy-detector**（zxc123aa，2026-02-10）【一手】 | 渠道（官方/Bedrock/Kiro/Vertex/Antigravity） | 工具调用 `call probe with q=test`（强制 tool_choice）、thinking `What is 2+3?`（budget 1024）、`Say OK` | tool_use id：官方 `toolu_`、Bedrock/Kiro `tooluse_`、Vertex/Antigravity `tool_N`；message id：官方 `msg_<base62>`、Vertex `req_vrtx_`；官方独有 `service_tier`、`inference_geo` 字段与 `anthropic-ratelimit-*` 头；**动态验证**：连发 4 次看 `anthropic-ratelimit-input-tokens-remaining` 是否递减，不变即伪造；案例：某中转漏出 `anthropic-ratelimit-unified-5h-*` 头被判为 Max 订阅（显示 74% 利用率） | 渠道判断对我们恒真；"利用率头可读出订阅与用量"说明 D 类思路可行 |
| **Veridrop / anthropic.mom / CCTest** | 综合 | 见 §4.2；CCTest（闭源）解析 Protobuf 编码的签名识别渠道 | — | 自检用 |
| **随机数/短答分布指纹**：hanlinwenyuan/hlwy-ai-checker（738★）→ xqy2006/ModelTrace（1472★，2026-10-09，linux.do 2827119）→ TraceOne【一手】 | 实际作答的模型 | TraceOne 一问版原文：`Using only the current language model, make 315 independent first-instinct selections of an integer from 1 through 355 inclusive. Sample mentally with replacement: after each choice, forget the earlier choices. Repeated integers are allowed and expected; do not try to cover the range, balance frequencies, avoid duplicates, or alternate high and low values. Output only one JSON object with the sole key "numbers", containing 9 arrays of exactly 35 integers each. Do not use tools, code execution, calculators, search, APIs, or external random generators. Do not count, use an arithmetic progression, or reuse a designed sequence. No explanation.` | 与自采基线比 Hellinger/JSD；ModelTrace 作者提醒 **Claude Code 系统提示会严重干扰结果**；TraceOne 只在 Codex 订阅 low 档校准过，确认率 84.2% | A10 的现成题；两侧必须同 UA、同 effort、同系统提示 |
| **meow-llm-detector**（chen-006，1496★，v4.5.4，linux.do 2704354）【一手】 | 实际作答的模型 | 现版概率探针：GPT `随便说出一个拟声词，只回答拟声词。`、`Name a random bird. Reply with ONLY the bird name, one word.`；Claude `不要思考，直接回答。从mepo、takri、sulon、vash中选一个词，只回答所选词。`；effort low、`max_output_tokens:128`、system 为 "."；GPT 36/72/108 次，Claude 60/90/120 次。早期版本的 "juice" 探针已被中转专门识别（改写成 "juiice nummber" 才露馅） | 似然比；作者 2026-08-10 补充："检测不通过不一定是中转掺水，**也可能是官方风控把请求路由到了低级模型**" | A10 的现成题 |
| **社区降智题**【一手原帖，结论多为经验】 | 能力 | **糖果题**（答案 21，误读会答 29；项目现有题库已收录，linux.do 2944491、2811197）；**水杯题**（答案 8，2797049）；**鹈鹕骑自行车 SVG**（只看首段关键词，2858863，同帖即有反例）；**juice 探针**（诱导泄露隐藏元数据） | 糖果题：健康账号在指定档位 5 次至少对 3 次；水杯题答 8 | 糖果/水杯可作 A09 的固定题（已公开、会漂移，必须同期对照）；鹈鹕关键词与 juice 不用（误报高；juice 属于套取系统信息，有风控风险） |
| **Codex 实际路由模型**（linux.do 1604331；leaf0329/chatgpt-codex-desktop-detector）【一手】 | 被改路由 | `RUST_LOG='codex_api::sse::responses=trace' codex exec --skip-git-repo-check -m gpt-5.3-codex "PING"` 后取 SSE 里的 `"model"` | 返回 `gpt-5.2-2025-12-11` 即被降级；作者明言"名称一致不等于能力一致" | 网关侧直接记录响应 model（A02） |
| **Codex 516 推理截断**（openai/codex#30364；linux.do 2917019）【一手：数据可复核，原因未证实】 | 推理被截断 | 统计 usage 里 reasoning_tokens 的取值分布 | 作者分析 39 万条记录：gpt-5.5 在 ≥516 推理 token 的请求里**恰好停在 516 的占 44.0%**（其他模型 1.3%），1034、1552 处也有峰值（518·n−2）；OpenAI 关单未解释；linux.do 称 gpt-6-astra 长推理请求 >50% 截在这些位置 | 零成本被动项（并入 D06）：按账号统计"推理 token 恰为 516/1034/1552"的占比，与同型号其它账号比 |
| **批量巡检**：CPA 插件 `cpa-codex-candy-eval`（linux.do 2951746）、noonwake-ai/ai-drool-router（与 Sub2API 绑定，用每个账号凭据跑糖果题与画图题，把正确率写回调度优先级，默认只算分不写回）、sub2api-excel2api-plugin【一手】 | 账号级降智巡检 | 糖果题末行要求 `最终答案：N`；帖中称降智账号有约 4 分钟"白名单窗口"，一轮测 5 次 | 正确率 | 与我们架构最像；印证"按账号巡检 + 写回调度"的可行性 |
| **ChatGPT PoW 难度**（KoriIku/chatgpt-degrade-checker，linux.do 261968、409884）【一手】 | 网页端账号风控 | 读 `/backend-api/sentinel/chat-requirements` 的 `proofofwork.difficulty` | 去前导 0 后 ≤2 位高风险…；反例不少 | 只适用网页端，**我们不用**（前文 §10 已述） |
| **Grok 风控标记**（linux.do 2645971；kaibush/grok-iq）【一手】 | Grok 账号被标记 | JWT 里的 `bfs` 机器人标记、页面 `botFlagSource`；`/imagine girl` 出随机图即被降智（社区经验） | `bfs` 为 1/2 或 `botFlagSource` 非 0 视为风控；TPS ≥150/500 判异常、推理输出为 0 | Grok 挂单的 A13 旁证（字段真实存在，"标记即降智"是社区观察） |
| **Claude Max 档位经验值**（linux.do 2835455、2990101）【一手帖，经验值】 | 档位 | 用 sub2api 折算 | 20x 指 5 小时窗口；周额度约为 Pro 的 5.25–8.8 倍（5x 约 3.5–4 倍）；Pro 周限约折 $250 | D03 档位反推时的先验，需自测 |
| **号池信号**【二手】 | 共用 | — | 订阅直连缓存命中率 92–98%，号池切号时缓存冷启动、命中率明显下降；ratelimit remaining 不单调递减说明多 key 轮询 | D14/D02 的旁证 |

**"官方对账号降智"的证据分级（子调研整理，§3 有一手原文）**：
- **有官方文档/官方承认**：Anthropic 的按请求回退与"账号信任信号"、对屡次违规用户的"增强安全过滤"（§3.2）；OpenAI 从 GPT-5.3-Codex 起把 cyber 高风险流量改路由到 GPT-5.2（§3.3）；Gemini CLI 免费层 Pro/Flash 混用与回退（gemini-cli Discussion #2436，团队成员回复）；Claude Code 2026-04 的客户端改动（前文 S03）。
- **数据可复核、原因未证实**：Codex 516 截断；2026-08～09 的"GPT 风控降智"（Codex 请求被路由到 gpt-5.6-luna 或知识截止 2024-06 的"神秘模型"，证据是糖果/水杯题答错、指纹 99% 匹配 luna、TPS 从 30–40 升到 60–100；有人抓到过模型标识 `gpt-5.6-sol-codex-abuse-1p-codexswic-ev3`，为单方说法；原因两派各执一词，OpenAI 客服不承认）。
- **传言或弱证据**："Claude 被标记账号被偷换成 Sonnet"（除文档化的按请求回退外无证据，社区也多说少见，2983050）；GLM/Kimi/MiniMax/OpenCode Go 套餐"量化/降智"（只有体感帖）；ChatGPT 网页 PoW 与降智的相关性（反例多）。
- **关于"Opus 掺 Sonnet"**：在公开中转里，它指中转把一部分 Opus 请求转给 Sonnet 以省成本（我们的 TLS 直连设计下不存在）。在官方订阅里，有一手依据的只有两种：①2025 年 Claude Code 客户端在 Max 用量阈值后自动改发 Sonnet（服务端只给 `-fallback` 信号，**客户端**行为，现行文档已无此行为，§5.1）；②2026 年按请求的安全回退（Opus 5/5.5 → Opus 4.8 或 Opus 5，Sonnet 5.5 → Sonnet 5），回退目标是**同系列的上一版**而不是 Sonnet，且写在 `model` 字段里（§3.2）。

---

## 5. 各厂商订阅：档位差异、官方降级/回退机制、可观测信号

### 5.1 Claude 订阅（OAuth，Claude Code 形态）与 ChatGPT/Codex 订阅

（子调研读了 Claude Code 官方文档与帮助中心、Claude Code 2.1.296 客户端二进制里的字符串（只检索不运行）、openai/codex 源码、sub2api v0.2.14 源码。**"一手·未文档化"**指官方客户端/源码里有、但官方文档没承诺的接口和响应头，随时可能变。）

**档位差异（截至 2026-10）**

| 项目 | Claude | ChatGPT / Codex |
|---|---|---|
| 档位取值 | `/api/oauth/profile` 的 `organization.organization_type`（claude_pro / claude_max / claude_team / claude_enterprise）+ `organization.rate_limit_tier`（客户端出现过 default_claude_max_5x、default_claude_max_20x、default_claude_zero）+ `seat_tier`、`has_extra_usage_enabled`【一手·未文档化】；**该接口需要 OAuth 授权含 `user:profile` 权限，只给 `user:inference` 的 setup-token 拿不到** | id_token（`https://api.openai.com/auth` 下）的 `chatgpt_plan_type`；`GET https://chatgpt.com/backend-api/wham/usage` 的 `plan_type`；429 响应体里的 `plan_type`。枚举含 free, go, plus, pro, prolite, promax, team, business, enterprise, edu 等（`protocol/src/account.rs`），显示名 prolite="Pro"、pro="Pro (More)"、promax="Pro (Max)"【一手：源码】 |
| 可用型号 | Claude Code 2.1.280（2026-09-22）起所有订阅档的 `default` 都是 Opus 5.5；Pro 自 v2.0.58 起可用 Opus【一手：https://code.claude.com/docs/en/model-config 、CHANGELOG】；**Fable 5/5.1**：Max 有单独的 Fable 周额度（客户端称 `seven_day_overage_included`，响应头缩写 `7d_oi`），Pro 未列此额度、走 usage credits；无 Fable 额度时上游 429 `error.details.error_code="credits_required"`；`/api/claude_cli/bootstrap` 返回 `model_access[{api_name, entitled, max_effort_level}]`【一手·未文档化】 | 型号目录（`models-manager/models.json`）：gpt-6-astra、gpt-6.1-sol、gpt-6-sol、gpt-6-luna、gpt-5.6-sol/terra/luna、gpt-5.5；隐藏的 cyber-permissive 变体属可信访问计划；**真实可用列表**由 `GET /backend-api/codex/models?client_version=` 按账号返回（源码注释："Discovery metadata does not grant access; inference still enforces authorization"）；不支持时 400 "The 'X' model is not supported when using Codex with a ChatGPT account."【一手：源码 + sub2api】 |
| 上下文 | Opus 4.7+、Sonnet 5+、Haiku 5.5、Fable **所有档原生 1M**；只有旧 Opus 4.6/Sonnet 4.6 要 `[1m]` 后缀且有档位/付费条件【一手：model-config】 | 上下文窗口 272K，`max_context_window` 872K（型号目录）【一手：源码】 |
| 推理强度 | effort 默认：Opus 5.5/Sonnet 5.5/Haiku 5.5 为 `medium`，Opus 4.7 为 `xhigh`；Enterprise 可按角色设上限（bootstrap 的 `max_effort_level`）【一手】 | effort 按型号给：Astra/Sol 系 low→xhigh/max/ultra，Luna 到 max；Astra 与 6.1 Sol 默认 low；**未见按档位限制 effort 的字段**【一手：源码】 |
| 速度 | fast mode 只支持 Opus 5.5/5/4.8，订阅档 "available via usage credits only"，实际速度看 `usage.speed`；用满后可选"低优先级模式"（请求头 `anthropic-usage-limit: slow`）【一手 + 一手·未文档化】 | Fast = 请求 `service_tier:"priority"`；实际以结束事件的 `response.service_tier` 为准【一手：源码】 |
| 用量窗口 | 5 小时会话 + 周限额，Max 另有 Fable 周限额，与 claude.ai、Cowork 共用；Team/席位制 Enterprise 按席位类型；2026-03-26 起工作日太平洋时间 5–11 点 5 小时额度消耗更快（周额度不变）【二手：The Register】——**估算容量应用 7 天窗口** | 第三方转述官方定价页【二手】：Plus 有 5 小时窗口 + 周限额，Pro 自 2026-09-29 起只剩周限额；个别型号有独立限额族（如 Spark `codex_bengalfox`） |

**官方会在什么情况下换型号**（§3.2、§3.3 已给一手原文，这里补字段）

- Claude：Opus 5.5 与 Fable 5.x 的网络安全请求回退到 Opus 4.8，生物/前沿 LLM 开发回退到 Opus 5；Sonnet 5.5 网络安全回退到 Sonnet 5；Opus 5 网络安全回退到 Opus 4.8；蒸馏类直接拦截。API 开启 `fallbacks` 后响应含 `fallback` 内容块与 `usage.iterations[].type=="fallback_message"`，之后约 1 小时同类请求粘在回退模型上；Claude Code v2.1.285 的修复说明写到 "server answered a refusal with a different fallback model"——**订阅流量里同样会发生**【一手】。2025 年的"Max 用户 Opus 用到阈值切 Sonnet"是**客户端**行为（服务端只给 `anthropic-ratelimit-unified-fallback: available` 与 `-fallback-percentage`），现行文档已无此行为。达到限额时**直接报错**并给重置时间，没有"悄悄换小模型"的机制【一手】。
- Codex：见 §3.3；达到限额 429 报错；Luna Reserve 由客户端改型号。

**可观测信号（字段级）**

1. **Claude 订阅额度响应头**（只在订阅/OAuth 流量出现，官方未文档化；前缀 `anthropic-ratelimit-unified` 省略）：汇总 `-status`（allowed / allowed_warning / rejected）、`-reset`、`-representative-claim`（five_hour / seven_day / seven_day_opus / seven_day_sonnet / seven_day_overage_included / overage）、`-fallback`、`-fallback-percentage`；分窗口 `-{5h,7d,7d_oi,overage}-utilization`（**0–1 小数**）、`-{w}-reset`、`-{w}-surpassed-threshold`、`-{w}-status`；超额 `-overage-*`；低优先级 `-slow-status`（active / not_needed / slot_busy / weekly_limit / budget_exhausted / ineligible / off）等【一手·未文档化：Claude Code 2.1.296 解析代码；sub2api 抓包样本 `ratelimit_service_anthropic_window_limit_test.go:92-112`】。官方客户端自己的"额度检查"是发一个 `max_tokens:1`、内容为 "quota" 的请求读响应头；社区建议**只在 5 小时窗口已开着时探测，免得替卖家"开窗"**。
2. **Claude 账号接口**：`GET /api/oauth/profile`（档位）、`GET /api/claude_cli/bootstrap`（型号权益与 effort 上限）、`GET /api/oauth/usage`（请求头 `anthropic-beta: oauth-2025-04-20`；five_hour、seven_day、seven_day_opus、seven_day_sonnet、extra_usage 等；**utilization 是 0–100，响应头里是 0–1**；限流较严）【一手·未文档化】；响应头 `anthropic-organization-id` 可与 profile 的组织 uuid 交叉核对，**发现"换了号"**。
3. **Claude 响应体**：`message.model` 是实际作答型号；`id` 前缀 `msg_01…`、`toolu_01…`、请求 ID `req_011…`（第三方渠道前缀表：`msg_bdrk_`、`msg_vrtx_` 等）；usage：input_tokens、cache_creation_input_tokens（细分 `ephemeral_5m/1h`）、cache_read_input_tokens、output_tokens（**含思考 token**）、service_tier、speed、inference_geo、server_tool_use、iterations；**提示总量 = input + cache_creation + cache_read**；拒答 `stop_reason:"refusal"` + `stop_details{category, explanation, recommended_model}`；`/v1/messages/count_tokens` 可用 OAuth 凭据调用、免费、有每分钟限制【一手（随 Claude Code 分发的官方 claude-api 文档）+ 一手·未文档化】。
4. **Codex 响应头与事件**：`x-codex-primary-used-percent`（**0–100**）、`-primary-window-minutes`、`-primary-reset-at`（Unix 秒），`x-codex-secondary-*` 同构；任意限额族 `x-<limit>-primary-used-percent` + `x-<limit>-limit-name`；`x-codex-credits-has-credits/-unlimited/-balance`、`x-codex-active-limit`、`x-codex-rate-limit-reached-type`（`codex-api/src/rate_limits.rs:56-231`）；**`OpenAI-Model`**；WebSocket 的 `codex.rate_limits` 事件；`response.completed` 的 usage（`input_tokens_details.cached_tokens`、`output_tokens_details.reasoning_tokens`）【一手：源码】。
5. **令牌复用**：OpenAI 刷新令牌时返回 `refresh_token_reused`（"refresh token was already used"）——**说明同一刷新令牌被别处用过**，是凭据被复制共用的强信号【一手：源码 `login/src/auth/manager.rs`、`app-server/tests/suite/auth.rs`】。
6. **tokenizer 代际**：Anthropic 自 Opus 4.7（2026-04-16）起换新 tokenizer（官方口径 "roughly 1.0–1.35× depending on the content type"【二手转引】），Sonnet 5、Haiku 5.5 相对 4.6/4.5 约多 30%；Opus 4.8、Opus 5/5.5、Sonnet 5.5、Fable 5/5.1 共用新 tokenizer【一手：随客户端分发的官方文档】。→ **输入计数只能区分"新一代"与"Opus 4.6/Sonnet 4.6/Haiku 4.5 旧一代"**，分不出 Opus 5.5 与其安全回退目标 Opus 4.8（后者本来就写在 `model` 字段里）。OpenAI：tiktoken 把 "gpt-5" 前缀映射到 o200k_base（覆盖 gpt-5.x、-codex），GPT-6 未公开【一手】。

**sub2api v0.2.14 已采集与缺口**（只读核对，tag 0363b8c）

- 已有：Claude `5h/7d/7d_oi` 利用率与重置时间写入账号 Extra（`service/ratelimit_service.go:2059-2105`）；429 时按 surpassed-threshold/利用率/`{w}-status=rejected` 判窗口；`7d_oi` 用满与 `credits_required` 只封 Fable；调 `/api/oauth/usage`；OpenAI 从 id_token 取 plan 并调 `/wham/usage`；`upstream_response_model` 采集 OpenAI `response.model`+`service_tier`、Anthropic `message.model`+`usage.speed`；usage_log 有 requested_model、upstream_response_model、upstream_model_mismatch、model_mapping_chain 与各项缓存 token（`ent/schema/usage_log.go:46-90`）；**上游连接禁止跳过 TLS 证书校验**（`pkg/httpclient/pool.go:126-129`，本文复核过；`handler/openai_live.go:231` 的 `InsecureSkipVerify` 是下游 WebSocket 的 Origin 校验开关，不是上游 TLS）——§1 "TLS 端到端"的前提在 HTTP 上游路径上成立，WebSocket 上游路径未单独核对。
- 缺口：**识别不了 Claude 的 Pro / Max 5x / Max 20x**（OAuth 只存 org_uuid、account_uuid、email，全仓库没有 `rate_limit_tier`）；不读 `representative-claim`、`fallback`、`overage-*`、`slow-*`；**不解析 `OpenAI-Model`**、`*-reset-at`、credits 头、`codex.rate_limits` 事件；`upstreamModelMismatch` 是严格字符串比较（`upstream_response_model.go:274`），上游返回带日期的型号名会被误判为不一致；usage_log 没有 reasoning_tokens、speed 列。


### 5.2 Google（Gemini CLI / Code Assist、Antigravity）、Grok、国产 Coding Plan、OpenCode Go

（以下由子调研读取官方文档与官方开源代码得出；"逆向"指来自 sub2api v0.2.14 等第三方实现，端点与字段不是厂商承诺，随时可能变。访问日期 2026-10-11。）

**先看几条会直接影响方案的事实**

1. **Gemini CLI 的个人 OAuth 已停服【一手】**：2026-06-18 起，Gemini CLI 与 Code Assist IDE 插件不再服务 Google AI Pro/Ultra 与免费的 Code Assist for individuals，个人用户迁到 Antigravity CLI（`agy`）；Code Assist Standard/Enterprise 许可不受影响（https://developers.googleblog.com/an-important-update-transitioning-gemini-cli-to-antigravity-cli ，2026-05-19；https://geminicli.com/docs/resources/quota-and-pricing/ 页顶横幅）。项目里"Gemini Google One"接入方式（`internal/listing/vendors.go` 的 `google_one`）与 sub2api `gemini_quota.go:190-206` 的旧档位表（Google One AI Pro 1500/日、Ultra 2000/日）都需要复核。
2. **多家条款明令禁止转售/中转/共用【一手】**：Antigravity FAQ："Using third-party software, tools, or services to access Antigravity is a violation of our Terms of Service … can result in suspension or termination"，且 "automating account rotation … is not supported under consumer plans"；Kimi Code 社区倡议："把自己的账号给别人转发（反向代理），属于违规"，处置先限并发、可能封号；GLM 使用须知禁止共享，"倒卖、中转、套利"可能永久封禁。→ 封号/限并发是寄售账号的系统性风险，应进入定价与 A13。
3. **官方自己会换型号，不能一律判降智【一手】**：GLM Coding Plan 把 GLM-5.2/5.1 自动切到 GLM-5.3、把 GLM-5-Turbo/4.7 切到 GLM-5.3-Flash；Kimi Code 关闭 thinking 后 K3/K2.8 请求都由"K2.8 Preview（无思考）"处理，高速版型号 ID 写错时"兜底到标准版，不报错"。→ A02 的"允许返回值映射表"要把这些官方路由列入白名单，质检只用各平台当前主力型号 ID。
4. **超额自动花真钱【一手】**：Antigravity 的 AI Credits 超额（Never/Always）、Kimi 加油包（触顶后"无缝"扣余额）、MiniMax 积分包（默认超额扣积分）、OpenCode Go 的 "Use balance"。买家流量可能直接消耗卖家余额——**上架时必须检测并要求关闭**（新增配置项，见 §7 A14）。sub2api 还会对开了 overages 的 Antigravity 账号注入 `enabledCreditTypes:["GOOGLE_ONE_AI"]`（`service/antigravity_credits_overages.go:114-125`）。
5. **唯一能用密码学方式证明"没被偷换模型"的是 Kimi 开放平台（按量 API）【一手】**：请求头带 `X-Msh-Request-Nonce`，响应头返回 `Msh-Request-Timestamp` 与 `Msh-Request-Signature`（`reqsigv1_` 前缀），再调 `POST https://api.moonshot.cn/v1/signatures/verify` 提交 `{nonce, timestamp, model, signature}` 得到 `{valid}`；文档原文说可用于核验代理层没有偷换模型（https://platform.kimi.com/docs/api/signatures-verify.md ）。Kimi Code 端点是否也返回这两个头**未知，需实测**。

**各平台要点**

| 平台 | 档位差异 | 官方降级/回退 | 可观测信号（字段/接口） | 证据 |
|---|---|---|---|---|
| **Gemini CLI / Code Assist** | Standard 每人每天 1500 次、Enterprise 2000 次，跨模型族合计（https://developers.google.com/gemini-code-assist/resources/quotas ，2026-10-09 更新）；预览模型权限不看档位名，看 `retrieveUserQuota` 返回的 buckets 里有没有该 modelId（gemini-cli `config/config.ts:2315-2381`）；模型 ID 是后端别名（`models.ts:144-164` 在 Code Assist 后端把 `gemini-3.5-flash` 改写为 `gemini-3-flash`） | **回退是客户端行为**：Pro→Flash 回退链（`availability/policyCatalog.ts:74-119`），默认弹窗询问，只有 Auto 模式瞬时错误才静默切换；**未见服务端静默换模型** | 所有接口 `POST https://cloudcode-pa.googleapis.com/v1internal:<method>`：`loadCodeAssist`（`currentTier`、`paidTier`、`ineligibleTiers[].reasonCode` 如 VALIDATION_REQUIRED/UNSUPPORTED_LOCATION）、`retrieveUserQuota`（`buckets[{modelId, remainingAmount, remainingFraction, resetTime}]`，上限可由 remainingAmount÷remainingFraction 反推）、`countTokens`（`totalTokens`）；生成响应含 `modelVersion`、`traceId`、`usageMetadata{promptTokenCount, cachedContentTokenCount, thoughtsTokenCount, …}`；错误 `ErrorInfo.reason`：RATE_LIMIT_EXCEEDED（可重试）、QUOTA_EXHAUSTED、**MODEL_CAPACITY_EXHAUSTED（服务端容量，不是账号问题）**、"limit: 0"（档位不含该模型）、403 VALIDATION_REQUIRED | 一手（文档 + gemini-cli 源码 commit 9b6e026） |
| **Antigravity** | 模型按档：Claude Sonnet/Opus 5.5 只给 Pro（非试用）/Ultra/Enterprise；Claude 4.6 与 GPT-OSS-120b 给 Free/Plus/Pro，2026-11-02 移除；Ultra $100 是 Pro 的 5 倍、$200 是 20 倍；Gemini 与"Claude and GPT"两个独立额度池，各显示周与 5 小时剩余百分比；"baseline rate limits are primarily determined by available capacity"（https://antigravity.google/docs/models 、/docs/plans） | 未见服务端换模型说明；**客户端**在 `--model` 无法解析时交互模式"回退并提示" | 【逆向】`fetchAvailableModels` 返回每个模型的 `quotaInfo{remainingFraction, resetTime}`、`maxTokens`、`supportsThinking`；`paidTier.id` 取 free-tier / g1-pro-tier / g1-ultra-tier；429 含 quota_exhausted vs `RATE_LIMIT_EXCEEDED`+retryDelay；503 MODEL_CAPACITY_EXHAUSTED；403 分 validation 与 violation（含 "terms of service" 视为封禁） | 档位一手，协议逆向 |
| **Grok** | SuperGrok $30/月（Grok 4.7）、SuperGrok Plus $100/月（"Priority access at peak times"）；不公布绝对限额（https://x.ai/pricing ） | 未找到自动降级说明 | 【逆向，sub2api `pkg/xai`】JWT tier：0=free、1=supergrok、3=x_premium、4=x_premium_plus、5=heavy、6=lite、7=plus；`GET /v1/billing?format=credits`（周 creditUsagePercent）与 `/v1/billing`（月度 monthlyLimit，$150≈SuperGrok、$1,500≈Heavy）；OAuth 默认并发 1。【一手，API 文档】`usage.cost_in_usd_ticks`（每请求实扣金额）、`POST /v1/tokenize-text` 分词接口 | 价目一手；OAuth 逆向 |
| **Kimi Code** | `k3` 需 Plus+（1M 上下文需 Pro+，否则 256K）、`kimi-for-coding`（K2.8 Preview，1M）、`kimi-for-coding-highspeed`（Pro+，6 倍速、3 倍消耗，官方称与标准版"是同一个模型"）；新会员只有 5 小时窗口 + 月额度，老会员另有周窗口；所有设备与 Key 共用一份额度（https://www.kimi.com/code/docs/ ） | 关 thinking → K2.8 无思考版处理；高速版 ID 错 → 兜底标准版不报错 | `GET https://api.kimi.com/coding/v1/usages`（`usage{limit, used/remaining, resetTime}`、`limits[]{window, detail}`，官方开源 kimi-cli `usage.py:80-200`）；错误：401（Key 无效或档位不含 k3/1M/高速版）、402（会员权益暂时无法验证）、403（额度触顶，或 "You've reached your concurrent request limit"——**由风控触发，与剩余额度无关**）、429（瞬时 too many requests 或 engine overloaded） | 一手 |
| **智谱 GLM Coding Plan** | 全档 GLM-5.3 与 GLM-5.3-Flash；2026-07-30 起积分制：Lite/Pro/Max 每 5 小时 2,000/12,000/28,000 积分、每周 10,000/60,000/140,000；高峰（工作日 14:00–18:00 UTC+8）全价、非高峰 5 折；并发 Max>Pro>Lite（https://docs.bigmodel.cn/cn/coding-plan/ ） | 旧模型自动路由到 5.3/5.3-Flash；关思考按 low 处理（"仍会轻量思考"）；OpenClaw 属"次级调度"，高负载排队 | 业务码：1302 并发限制、**1305 平台过载**、1308 5 小时上限、1310 周/月上限、1309 套餐到期、**1311 当前套餐未开放该模型**、**1313 不符合公平使用策略（风控）**、1113 欠费；`POST https://open.bigmodel.cn/api/paas/v4/tokenizer`；【逆向】`GET /api/monitor/usage/quota/limit` 返回 `data.level` 与 `limits[]{type, unit(3=5h,6=周), percentage, nextResetTime}` | 一手；额度接口逆向。媒体转述：2026-01 因并发与速度问题限售至 20%，2026-02 致歉信称"遭受灰产号池和黄牛党冲击"【二手】 |
| **MiniMax M Plan** | Go / Explore（Go 的 3 倍）/ Build（12 倍），文本统一 MiniMax-M3.1-Flash-Preview；5 小时与周窗口从首次使用起算；"额度充足仍可能限流"（https://platform.minimaxi.com/docs/m-plan/ ） | 未见换模型；超额默认扣积分包 | 错误：1002 频率超限、2045 频率增长超限、1041 连接数限制、2056 超出 M Plan 资源限制；响应头 `trace_id`；`GET https://www.minimax.cn/v1/token_plan/remains`；官方 TPS 公式 = 输出 token 数 ÷（末 token 时间 − 首 token 时间） | 一手；sub2api 仍用旧域名 `api.minimaxi.com`，需复核 |
| **OpenCode Go** | Go $10、Go Plus $40；按模型设月度美元上限，5 小时 = 月度 20%、周 = 50%；由 OpenCode 选定的第三方推理商托管，未披露量化精度（https://opencode.ai/docs/go/ ） | 超额可开 "Use balance" 回落扣 Zen 余额 | 【逆向】`GET https://opencode.ai/zen/go/v1/usage`（`usage.{rolling, weekly, monthly}.{percent, resetsAt}`）；必须发稳定的 `x-opencode-session` | 一手；"模型变笨"的说法二手 |

**经 Antigravity 调用 Claude 时，"是不是真 Claude/哪一款"不能用 Anthropic 的那套判据【逆向，sub2api v0.2.14】**：上游返回的是 Gemini 信封（candidates、usageMetadata、modelVersion、responseId）；sub2api 转换成 Anthropic 格式时，id 用上游 responseId，缺失时**生成假的 `msg_01` + 22 位 Base62**（`pkg/antigravity/response_transformer.go:292-299,377-389`），model 字段**回显客户端请求的名字**而非上游 modelVersion（`:303`），usage 换算为"输入 = promptTokenCount − cachedContentTokenCount，输出 = candidates + thoughts"；thinking 签名是 Google 的 thoughtSignature，拿不到 Anthropic 去验；历史消息缺真实签名时网关会**把 thinking 降成普通文本并关闭 thinking**（`request_transformer.go:478-530`）——这是"网关自己造成的降智"；默认映射还会静默换型号（`domain/constants.go:106-170`：claude-haiku-4-5 → claude-sonnet-4-6、claude-opus-4-5 → claude-opus-4-6-thinking 等）。→ 对 Antigravity 挂单，A02 仅使用现有响应或只读用量记录可取得的**上游原始 modelVersion**；缺失时记“无法判定；依赖 sub2api，后续处理”，不修改网关；A03 的计数口径不同；`msg_` 前缀/签名不能作真伪证据。

**国产 Coding Plan 的"是否与按量 API 同款"**：GLM 文档以"相较按量调用 GLM-5.3 标准 API 最高省 92%"比价、用同一套计价折算积分，MiniMax FAQ 写"已有按量计费价格的 API 端点会按对应按量计费价格扣减套餐内额度"——**都隐含同一端点同一模型，但没有找到"未量化"的明确声明**。Moonshot 的 KVV 官方博客称第三方部署与官方 API 的差异"普遍存在"，其方法（先校验采样参数被正确执行，如 Thinking 模式强制 temperature=1.0、top_p=0.95；再测工具调用 F1 与 Schema 准确率；再跑 AIME 长输出）可直接用于"Coding Plan vs 按量 API"的配对检验（§2.9）。

**429 要分四类，只把前两类算到账号头上（子调研整理）**：

| 类别 | 典型信号 | 处理 |
|---|---|---|
| 配额耗尽 | GLM 1308/1310；Kimi 403 额度触顶；Google QUOTA_EXHAUSTED；Anthropic/Codex 窗口利用率到顶 | 计入 D04/D05 |
| 限速/并发 | GLM 1302；Kimi 429；Google RATE_LIMIT_EXCEEDED；MiniMax 1002/2045/1041 | 计入 D04/D11 |
| 平台容量 | GLM 1305；Kimi engine overloaded；Google MODEL_CAPACITY_EXHAUSTED；Anthropic 529 overloaded | **不扣卖家分**（官方全局） |
| 风控 | GLM 1313；Kimi 403 concurrent limit；Google 403 VALIDATION_REQUIRED / ToS；Anthropic 身份验证要求 | A13：停止调度 |

**sub2api 相关的待核对点**：Gemini 只有静态档位表、不调 `retrieveUserQuota`（`gemini_quota.go:190-206`）；MiniMax 默认 base_url 仍是 `api.minimaxi.com`（`service/domain_constants.go:78-101`）；对 Kimi/GLM/MiniMax 开了 thinking 但没传 effort 的请求，usage_log 里一律写 "high"（`handler/gateway_handler.go:582-591`），**不代表上游实际档位**（GLM 实际默认 max）——用它做推理档位统计会错。

---

## 6. 技术逐项分析：检测什么、怎么出题、读什么字段、怎么判、花多少、会不会误报、适不适合我们

说明：成本按"一次检测、单个被测账号"估算，**基线账号同批次再花一份**（配对设计，前文 §5.1）。token 数为量级估计，未计网关为 OAuth 自动加的固定前缀（sub2api v0.2.14 会给 Claude OAuth 请求加一段 `You are Claude Code, Anthropic's official CLI for Claude.` 系统块，见 `backend/internal/service/gateway_service.go:41`、`gateway_claude_oauth_body.go:575`）。"适用性"一栏对应 §1 的威胁模型。

| # | 技术 | 检测什么 | 探针设计 | 读取字段 | 自动判分 | 成本（单侧） | 误报风险 | 适用性 |
|---|---|---|---|---|---|---|---|---|
| T1 | 身份自报（banner grabbing） | 自称的厂商/型号/截止日期 | "Who created you?" 等 | 文本 | 字符串匹配 | 1–5 次，<1K token | 极高：系统提示可覆盖、模型常自称错（Cai 等 <5%；LLMmap Table 1） | **不用于判定**；最多作为风格特征 |
| T2 | 风格指纹分类（Idiosyncrasies、LLMmap、BazaarLink 风格题） | 哪一家；同家族哪一款（中等） | 开放式问题、列表、代码、解释类 | 文本（首词、短语、markdown） | TF-IDF+逻辑回归或嵌入分类器，**用基线账号样本训练** | 每题 300–1500 输出 token，需 10+ 题 | 中：客户端系统提示、版本更新、风格漂移都会改变；同家族近亲区分有天花板（BazaarLink 自述） | 家族识别不需要；型号识别作**仅提示**的辅助 |
| T3 | 分布等价检验（MET，Hamming-MMD） | 被测与基线的输出分布是否相同（换型号、温度被改、微调、部分量化） | 25 个随机片段续写，temperature=1，L≈50 token，每提示 10 样本 | 文本 | 置换检验 p 值；先做 A/A 校准 | ~250 次请求、~2.7 万 token | 中：官方不同集群/硬件的数值差异可能被检出为"不同"；小比例掉包测不出（Cai 等） | 适合作二阶段触发器，**不单独处罚** |
| T4 | 能力阶梯 / 能力配对 | 同家族降档（大→小）、推理被压低导致的答题率下降 | 参数化生成、可程序判分的题（见 §7 A09） | 文本 + stop_reason | 题目级配对差异与区间（前文 §6） | 30–60 题，每题 50–2000 token | 中：题目饱和、截断、拒答要单独分类；单次小样本功效低 | **核心**（前文已详述统计） |
| T5 | 冷门知识容量（IKP） | 掉到小一档模型（参数量量级变化） | 冷门事实短问答（Wikidata 年份等） | 文本 | 正确率配对；数字答案精确匹配 | 100–400 题，可每请求批 20 题 | 中：拒答策略变化会压低分数；同代近邻分不开（Sonnet 4.6≈Opus 4.7） | 只抓大落差 |
| T6 | **知识边界指纹 KBF** + 幻觉相似度 HSS（§2.6、§2.7） | **是不是同一个服务端模型**（双胞胎复核），对部分请求替换也敏感 | 由基线账号生成并筛出"它自己能稳定复现"的数值填空题；每请求 10–20 题，温度 0 | 文本 | KBF：与基线记录答案的一致率 vs 基线自身一致率，单侧二项检验；HSS：共同答错题上错得一样的比例 | 10–20 次请求、约 2 万 token（KBF 报告 $0.24–0.67/次） | 低到中：不同硬件池有少量差异，阈值必须来自 A/A；KBF 报告 16 个同参考对照 0 误报 | **很适合**：不受系统提示和风格影响，题目长得像普通问答 |
| T7 | 知识截止 / 时事指纹 | 版本代际（旧版/新版） | 截止前后若干月的事实题 | 文本 | 与同时段基线一致率 | 5–20 题，短答 | 中高：时事答案随时间变，必须同时段配对 | 仅提示 |
| T8 | **tokenizer / 输入计数指纹** | 型号代际（tokenizer 是否变化）、请求是否被额外注入 | 固定多语种文本（中英文、代码、emoji、数字）+ 固定模板，max_tokens=1，不开思考；外加"+80 个 apple"差分 | Anthropic：`input_tokens + cache_creation_input_tokens + cache_read_input_tokens`；OpenAI Responses：`usage.input_tokens`；Gemini：`usageMetadata.promptTokenCount` | 与同模型基线**精确相等**；或与历史表中各代际的计数比对 | 2 次，几百 token | **低**（确定性计数）；差异也可能来自网关/客户端模板变化，需先排除 | **强烈推荐**，几乎零成本；但 Claude 只能分新旧两代、Codex 只能查注入（§5.1） |
| T9 | 隐藏注入（token inflation） | 官方或网关在请求里加了看不见的内容 | 最短提示 "Hi" | 同 T8 | 与基线差值 > 0 即异常 | 1 次 | 低 | 推荐（并入 T8） |
| T10 | 响应元数据 | 实际服务的型号、渠道 | 任意请求（含真实买家流量） | Anthropic `message.model`（+ `fallback` 块、`usage.iterations`）、`id` 前缀、`anthropic-organization-id` 头；Codex 响应头 **`OpenAI-Model`**（`response.model` 是否随改路由变化未核实，官方客户端以头为准）；Gemini/Antigravity 上游原始 `modelVersion`；sub2api `upstream_response_model` | 与请求型号不一致即标记（先建"别名→快照名"与官方自动路由白名单） | 0（被动） | 低；Antigravity 路径的 model/id 是网关伪造的（§5.2），不能用 | **强烈推荐**，被动全量 |
| T11 | 推理量审计 | effort/思考预算是否被尊重 | 固定 effort 的中等难度题，最终答案只要一个数 | OpenAI `output_tokens_details.reasoning_tokens`；Claude `usage.output_tokens`（含完整思考，展示的是摘要，前文 S10）；Gemini `thoughtsTokenCount` | 与基线中位数比值、分题配对 | 10–20 题，每题 1K–8K token | 中：推理量本身方差大；官方会调整默认 effort（前文 S03） | 推荐（掺水核心） |
| T12 | 上下文上限与截断 | 档位宣称的上下文是否可用、是否被截断 | 恰低于/略高于宣称上限的请求 + 多针检索 | HTTP 状态与错误类型、`input_tokens`、答案 | 接受与否是否符合档位；`input_tokens` 等于预期；检索正确 | 很贵（接近上限的输入），只在上架/申诉做 | 低（错误类型明确）；注意官方计数与本地估算差异 | 推荐（定档） |
| T13 | 提示缓存功能 | 缓存是否生效（影响买家成本） | 同一长前缀（≥最小可缓存长度）连发两次 | `cache_creation_input_tokens`、`cache_read_input_tokens`；OpenAI `input_tokens_details.cached_tokens` | 第二次读取量 > 0 | 2 次，各数千 token | 低；缓存有路由/过期的随机性，失败要重试 | 推荐（功能项） |
| T14 | 速度指纹 | 型号大小、服务档位、共用导致的排队 | 固定长度输出（如重复单词 200 次） | TTFT、输出 token/秒 | 与同时段基线比 | 1–3 次，~500 token | 中：网络、代理、官方负载都影响 | 推荐（性能项，非真伪证据） |
| T15 | **限额/用量头分析** | 真实档位、他人共用、剩余额度 | 不额外发请求，读每次请求的响应头/不耗额度的用量接口 | Claude `anthropic-ratelimit-unified-{5h,7d,7d_oi}-utilization`（0–1）/`-reset`/`-status`/`-representative-claim`、`/api/oauth/usage`（0–100）；Codex `x-codex-{primary,secondary}-used-percent`（0–100）/`-reset-at`/`-window-minutes`、`/wham/usage`；国产各家额度接口（§5） | 档位：同等成本下的利用率增量（1/k=容量）；共用：空闲期上涨、窗口起点早于平台首请求、回归残差 | 0（被动） | 低到中：需要精确掌握平台自身对该账号的用量；官方额度跨产品共享，卖家自用也会被测出（这本身就是共用） | **强烈推荐**（掺水核心） |
| T16 | 改路由 / 安全回退 / 拒答 | 官方因账号信任度低而把请求回退到弱模型或拒答 | 被动读真实流量 + 5–10 条无害对照题 | Claude `stop_reason:"refusal"`+`stop_details.category`、回退后的 `model`；Codex `OpenAI-Model`、`ModelReroute`、`openai_verification_recommendation`、400 `cyber_policy`（§3.2、§3.3） | 无害题上出现即账号级标记；真实流量按内容类别分层后与同型号比 | 0 + 少量 | 低（字段明确）；需区分买家内容触发 | **强烈推荐** |
| T17 | logprob 跟踪、B3IT 边界输入 | 微小分布变化 | 见前文 S13/S14 | logprobs（多数订阅接口没有） | 序贯检验 | 低 | — | 订阅接口多半不可用（前文已述） |
| T18 | TEE / 可验证推理（TOPLOC 等） | 权重完整性的密码学证明 | — | 需要厂商配合 | — | — | — | **不可用**：闭源官方不提供（Cai 等） |
| T19 | 供应链/注入探针（2604.08407 类） | 中转改包、偷 key | canary、安装命令、条件触发 | 文本 | 精确匹配 | 每项 1 次 | — | 卖家侧**不适用**；只做平台自检 |
| T20 | 回放缓存检测（temperature>0 两次结果相同） | 中转返回缓存 | 随机数生成 | 文本 | 是否完全相同 | 2 次 | — | 卖家侧不适用 |
| T21 | 随机串/单 token 分布指纹（IRIS、FLIPS、One Token、ModelTrace/TraceOne、meow） | 换型号、部分请求被稀释（ε）、系统提示/采样参数被改 | "给 300 个 1–355 的整数"、16 位随机比特串、"1–100 随机选一个数"、"随便说一种鸟" | 文本 | 与**自采**基线比直方图距离/似然比；稀释检验约需 1/ε 次查询（IRIS：ε=10% 约 32 次） | 1–100 次短请求 | 中：对 effort、系统提示、温度敏感，两侧参数必须完全一致；服务端硬件差异也会被测出 | 推荐作便宜的二阶段触发器（A10） |
| T22 | 服务端参数规则指纹（wxmb01 S2/S8/S14） | 型号代际 | 非法 effort、特定 thinking 类型、最小可缓存长度扫描 | HTTP 状态、`cache_read_input_tokens` | 与自测规则表逐项一致 | 每项 1–4 次 | 低（确定性）；规则随版本变化，需维护 | 推荐（A15） |
| T23 | 工具偏好分布（AgentProv） | 换骨干模型（对系统提示注入稳健） | 每个模板提供功能等价的多个工具，记录选择 | 工具调用块 | MMD 置换检验 | 100–400 次短调用 | 低（论文：注入系统提示时误报 7%） | 适合 Claude Code/Codex 流量，可选 |
| T24 | 缓存侧信道/共用检测（CacheTracer、KeyPooling） | 有人在共用账号；跨买家缓存泄露 | 被动：公共前缀的"无法解释的缓存命中"；主动：时延 KS 检验 | `cache_read_input_tokens`、`cached_tokens`、TTFT | 平台 TTL 内未写过却命中 → 外部使用 | 被动 0；主动约 500 次 | 中：缓存域范围（账号/组织）需先验证 | 推荐（D14）；跨买家泄露属平台自检（S06） |

---

## 7. 质检项清单（真伪 A 类 + 掺水 D 类 + 平台自检 S 类）

### 7.0 怎么读这份清单

- **"配置项"与"成色项"分开**（借二手 MacBook 的说法）：档位、可用型号、上下文、功能这类是"配置"，决定挂单属于哪个 SKU、基础倍率是多少——实测与宣称不符时**按实测重新定档**，严重虚报才一票否决；共用、速度、推理量、能力、稳定性是"成色"，在基础倍率上扣分。
- **严重程度**（起步建议，需用 A/A 与已知干预校准，前文 §5.4）：
  - **一票否决**：不得按宣称上架，或立即停止调度直到复测通过；
  - **重扣**：倍率下调一档（起步建议 −15%～−30%）；
  - **轻扣**：−3%～−10%；
  - **仅提示**：只在报告展示，不影响价格。
- **归因**：每项注明责任方。只有"卖家可控"（虚报档位、共用、额度见底、代理质量）计入卖家信用；"官方"（改路由、全局降智、档位政策）影响价格但不罚卖家；"平台"（网关配置）不计入卖家。归因方法沿用前文 §8 的账号×出口交叉。
- **成本**写"单侧"，基线账号同批次再花一份。token 为量级估计。
- **探针总原则**（§3.1、§3.2、§4.2）：无害内容；不含越狱/注入串；不索要推理过程；参数化生成、题库轮换；随机时间、低频；请求形态与真实买家一致；单账号每天探针量控制在真实流量的很小比例。

### 7.1 总表

> **候选目录，不是本期处罚表**：下表保留原研究 A/D/S 编号、成本、阈值与频率。确认方案中 A01 只展示令牌可读档位，Claude 标“未核验”；A11 功能缺失按功能组扣价，基线也不支持则“不适用”，不是新增 SKU 一票否决；D05 只展示余量；A07/A10/A14、D01–D03/D11/D14/S 类均列为后续。表中的一票否决、20% 外部消耗、0.7 推理比与 max/low 1.3 倍等历史建议不能自动启用。实际门槛、系数、频率和预算以[汇总报告](../quality-inspection-2026-10-12.md)及[数据契约](data-contract.md)为准。

| id | 名称 | 类别 | 严重程度（失败时） | 单侧成本 | 建议频率 |
|---|---|---|---|---|---|
| A01 | 凭据与订阅档位声明核验 | 配置 | 一票否决（虚报一档以上）/ 按实测重定档 | 0–1 次请求（读凭据/配额接口） | 上架、每日 |
| A02 | 响应型号字段一致性 | 真伪 | 重扣（持续不一致）/ 仅提示（偶发） | 0（被动） | 全量被动 |
| A03 | 输入计数指纹（tokenizer + 隐藏注入） | 真伪 | 仅提示→复核后重扣 | 2 次，约 1K token | 每次检测 |
| A04 | 型号权限矩阵 | 配置 | 对应型号一票否决（不得挂该型号） | 每型号 1 次，<100 token | 上架、每周 |
| A05 | 上下文上限核验 | 配置 | 按实测重定档；宣称 1M 实为 256K 等一票否决 | 1 次拒绝探针（不计费）；仅对存在上下文档位差的型号（如 Kimi k3 1M、旧 Opus 4.6 `[1m]`）再做 1 次略超下档上限的跨档探针（约 26 万 token） | 上架、申诉 |
| A06 | 推理/思考档位可用性 | 配置 | 按实测重定档 | 每档 1–2 次，各 1K–8K token | 上架、每周 |
| A07 | 知识边界指纹（KBF + HSS，双胞胎复核） | 真伪 | 重扣（确认不同服务且可复现）；单次仅提示 | 10–20 次请求，约 2 万 token | 上架、申诉、二阶段触发 |
| A08 | 知识代际/时事配对 | 真伪 | 仅提示 | 5–10 次短答 | 每周 |
| A09 | 能力阶梯（同家族降档） | 真伪/掺水 | 重扣（配对确认）/ 疑似 | 30–60 题，约 3–8 万 token | 定时（前文 §5.3） |
| A10 | 输出分布指纹（随机数直方图 / MET-lite） | 真伪 | 仅提示（触发二阶段） | 1–3 次，各约 1.5K token；MET-lite 约 250 次 | 每周 / 触发时 |
| A11 | 功能项（图片、PDF、工具调用、结构化输出） | 配置 | 按实测重定档（缺功能则该 SKU 不可售） | 4–6 次，各 <1K token | 上架、每周 |
| A12 | 改路由/安全回退/拒答率（含无害对照题） | 真伪 | 重扣（无害题也被回退/改路由，或显著高于同型号）/ 仅提示（买家内容触发） | 被动 0 + 5–10 条短题 | 全量被动 + 每次检测 |
| A13 | 账号风控状态 | 可用性 | 一票否决（需身份验证/封禁/地区限制期间） | 0（被动错误分类） | 全量被动 |
| A14 | 超额自动扣费开关 | 配置 | 未关闭且未计价则一票否决 | 0–1 次查询 | 上架、每日 |
| A15 | 服务端参数规则指纹 | 真伪 | 仅提示（转 A07） | 每项 1–4 次，<5K token | 上架、每周 |
| D01 | 窗口起点推断（他人先用） | 共用 | 重扣（声明独占时一票否决） | 0（被动） | 每个窗口 |
| D02 | 空闲期用量漂移 | 共用 | 重扣 | 0–1 次用量查询 | 空闲时每 15–30 分钟 |
| D03 | 用量增量对账（外部消耗占比 + 档位反推） | 共用/配置 | 外部占比 >20% 重扣；反推档位不符转 A01 | 0（被动统计） | 每日汇总 |
| D04 | 429/限流画像 | 共用/性能 | 轻扣～重扣 | 0（被动） | 全量被动 |
| D05 | 剩余额度（"电池健康度"） | 容量 | 轻扣 / 限制可售量 | 0–1 次查询 | 每次调度前缓存 |
| D06 | 推理量配对 | 掺水 | 重扣（配对确认）/ 疑似 | 10–20 题，约 3–10 万 token | 定时 |
| D07 | 能力配对回归 | 掺水 | 重扣 / 疑似（前文 §6） | 同 A09（共用题目） | 定时 |
| D08 | 输出截短/懒惰 | 掺水 | 轻扣 / 仅提示 | 0（被动）+ 2–3 次计数题 | 被动 + 每周 |
| D09 | 有效上下文（多针检索） | 掺水 | 重扣（低于档位应有能力） | 很贵（数万到数十万 token） | 上架、申诉 |
| D10 | 速度配对（TTFT/TPS） | 性能 | 轻扣～重扣 | 1–3 次，约 0.5K token | 每次检测 + 被动 |
| D11 | 并发承载 | 性能/共用 | 轻扣 | 2–4 个并发小请求 | 上架、每周 |
| D12 | 提示缓存功能 | 配置/成本 | 轻扣（缓存不生效影响买家成本） | 2 次，各数千 token | 上架、每周 |
| D13 | 稳定性（错误率、5xx、断流） | 性能 | 轻扣～重扣 | 0（被动） | 全量被动 |
| D14 | 无法解释的缓存命中 / 刷新令牌被复用 | 共用 | 重扣（多次或出现 `refresh_token_reused`）/ 仅提示（单次缓存命中） | 0（被动） | 全量被动 |
| S01–S06 | 平台自检（证书、协议转换、签名回传、模型映射、注入抽检、缓存隔离） | 平台 | 不计入卖家 | 少量 | 每日/发版后 |

### 7.2 真伪类（A）逐项

**A01 凭据与订阅档位声明核验**

> 官方接口路径是厂商资料，不表示 token-market 获得直连授权。凭据仍由 sub2api 保管；本期仅读取现有接口/只读数据已提供的档位，Claude 未核验。以下官方 profile/bootstrap 核验与 D03 容量反推属后续，现有接口不提供时标“依赖 sub2api，后续处理”，不改授权流程或 sub2api 实现。
- 检测什么：卖家宣称的档位（Claude Pro/Max 5x/Max 20x/Team，ChatGPT Plus/Pro/Business，Google AI Pro/Ultra/Code Assist，Grok 各档，各家 Coding Plan Lite/Pro/Max）是否属实。
- 研究候选：不发推理请求，由保管凭据的现有上游集成读取官方档位资料（官方 TLS 链路的机制见 §5；平台不直接使用卖家凭据访问厂商）：
  - Claude：`GET /api/oauth/profile` 的 `organization_type` + `rate_limit_tier`（如 `default_claude_max_20x`）+ `seat_tier`；`GET /api/claude_cli/bootstrap` 的 `model_access[].entitled`；被动旁证：响应里出现 `7d_oi` 窗口头 → 有 Fable 周额度（基本是 Max），请求 Fable 得到 `credits_required` → 订阅不含 Fable。**需要 OAuth 授权含 `user:profile` 权限**，setup-token 方式拿不到，只能靠被动旁证与 D03 容量反推。
  - ChatGPT/Codex：id_token 的 `chatgpt_plan_type`（每次刷新都会更新）、`/wham/usage` 的 `plan_type`、`/backend-api/codex/models` 返回的型号列表。
  - Gemini Code Assist：`loadCodeAssist` 的 `currentTier`/`paidTier`；Antigravity：`fetchAvailableModels` 的型号清单与 `paidTier.id`；Grok：JWT tier 字段 + `/v1/billing` 月度限额；Kimi：越权探针（请求高速版或 1M 的 k3，401 即档位不够）；GLM：`quota/limit` 的 `data.level` 与 1311 错误；MiniMax：`token_plan/remains`（§5.2）。
  - 现状：项目里 Claude OAuth 的 `tier` 取值为 `none`（`internal/listing/vendors.go`），sub2api v0.2.14 也不取 `rate_limit_tier`——**Claude 档位目前完全没有核验**，本期明确披露，不承诺补取；当前 HEAD 对照见[网关核对](sub2api-rewrites.md)。
- 判分：声明档位 = 凭据/接口档位 → 通过；不一致 → 以官方值为准重新定档；差一档以上且卖家坚持宣称 → 一票否决。另用 D03 的"用量增量反推档位"交叉验证（凭据字段可能滞后于降级/退订）。
- 成本：0–1 次查询。频率：上架、每日（订阅可能中途降级或到期）。
- 误报：订阅刚升级/降级时字段延迟；团队席位与个人订阅字段格式不同。

**A02 响应型号字段一致性（被动）**
- 检测什么：官方实际服务的型号是否与请求一致（改路由、限额回退、网关模型映射）。
- 探针：不额外发请求。对**所有真实买家请求和探针**记录现有响应或只读用量记录可取得的实际作答型号：Anthropic `message_start.message.model`（开启回退时还有 `fallback` 内容块与 `usage.iterations[].type=="fallback_message"`）；Codex 响应头 **`OpenAI-Model`** 是官方客户端判据，但 sub2api 目前不解析/透传，记“依赖 sub2api，后续处理”；Gemini/Antigravity 只有现有记录可取的**上游原始** `modelVersion` 可用，取不到则无法判定（Antigravity 转 Claude 时 sub2api 回显的 model 不可信，§5.2）；Coding Plan 的 `model`。sub2api 已有 `upstream_response_model` 与不一致标记，但它是严格字符串比较（带日期的型号名会误报）。
- 判分：维护"请求型号 → 允许的返回值"映射（别名↔带日期快照；**官方公开的自动路由**如 GLM 旧型号→5.3、Kimi 关思考→K2.8 无思考版列入白名单）。按账号统计"不一致且属于安全回退/改路由"的比例，与同型号其它账号、基线账号比较（置换检验）。某账号显著偏高 → 重扣并转 A07/A12；偶发且能对应到买家内容（例如买家确实在做安全测试）→ 仅提示。
- 成本：0。
- 误报：别名更新导致的假不一致要靠映射表维护；Claude 的回退会粘在整个会话上（§3.2），同一会话的多次回退只算一次。

**A03 输入计数指纹（tokenizer + 隐藏注入）**
- 检测什么：①同一请求的输入 token 数是否与基线账号**完全相同**（不同 → 型号/tokenizer 不同，或请求在某处被额外注入）；②计数是否落在该型号的已知值上（识别 tokenizer 代际）。
- 探针（固定模板，max_tokens=1，不开思考，temperature 默认）：
  ```
  请只回复 OK。
  [fp-v3] 量子纠缠与暗物质的关系尚无定论；Pneumonoultramicroscopicsilicovolcanoconiosis;
  def f(x): return x**2 + 3*x - 7  # 注释 ✓
  👨‍👩‍👧‍👦 🏳️‍🌈 1234567890123456789 3.14159265358979 https://example.com/a?b=c&d=e
  カタカナ ひらがな 한국어 العربية Ελληνικά
  ```
  另发一条"同模板 + 追加 80 个 ` apple`"做差分（veridrop 的方法），差值只取决于 tokenizer，能抵消网关/客户端加的固定前缀。
- 读取：Anthropic `input_tokens + cache_creation_input_tokens + cache_read_input_tokens`（网关可能给系统块加了 `cache_control`，只看 `input_tokens` 会漏算）；OpenAI `usage.input_tokens`（`cached_tokens` 是其子集，不要相加）；Gemini `usageMetadata.promptTokenCount`。
- 判分：总数与基线**精确相等**才通过；差分值等于该型号表中的值。按 AgentProv 的判读规则（§2.7）：与基线逐条差值全为 0 正常；恒定正差 k = 某处多注入了 k 个 token；负差 = 模板更精简/截断；有方差 = 后端不一致。不等 → 先排除网关/模板版本差异（同批次两侧走同一网关，应当相同），仍不等 → 记为"计数异常"，转 A07 复核，复现两次以上重扣。
- **能认出什么、认不出什么**（§5.1）：Claude 自 Opus 4.7 起换新 tokenizer，计数只能区分"新一代（Opus 4.7/4.8/5/5.5、Sonnet 5/5.5、Haiku 5.5、Fable）"与"旧一代（Opus 4.6、Sonnet 4.6、Haiku 4.5）"，分不出 Opus 5.5 与回退目标 Opus 4.8；GPT-5.x 全系 o200k_base，**对 Codex 只能查注入、不能认型号**。可用 `count_tokens`（OAuth 可调、免费、限频）分别按新旧型号计数，建立期望值表。
- 成本：2 次，合计约 1K token。频率：每次检测都做（几乎免费）。
- 误报：低。需要在每次网关升级、模板变更后重建期望值表；新型号上线先建表再判。

**A04 型号权限矩阵**
- 检测什么：挂单宣称可用的每个型号是否真的能调（例如 Pro 是否能用最高档型号、某些 effort/快速模式是否只对高档开放）。
- 探针：对每个宣称型号发 `Reply with exactly: OK`，max_tokens=8（推理型号用最低 effort）。
- 判分：成功 → 可售；明确的"该订阅不可用此型号"错误 → 该型号一票否决（不得挂）；429/限额 → 不判，稍后重试；返回体的 model 与请求不一致 → 转 A02。
- 成本：每型号 1 次，<100 token。频率：上架、每周、官方发布新型号后。

**A05 上下文上限核验**
- 检测什么：账号是否真的支持宣称的上下文（如 Kimi `k3` 的 256K 与 1M、旧 Opus 4.6 的 200K 与 `[1m]` 这类档位差；没有档位差的型号只做拒绝探针与 `input_tokens` 核对）。
- 探针（两步，先便宜后贵）：
  1. **拒绝探针**：构造略高于宣称上限（+2%）的输入，期望官方返回"prompt too long"类 400 错误（不计费，几乎零成本）。如果反而被接受，说明上限比宣称更大或计数口径不同，记录。
  2. **跨档探针**：构造略高于**下一档**上限（例如宣称 1M 的 Kimi `k3` 就发约 26 万 token，高于 256K 档）的输入 + 结尾一个问题，max_tokens=16。被接受 → 证明不是低档；被拒 → 实为低档。不必真的发接近 1M 的请求。
  长文本用随机生成的无意义但可分词稳定的文本（避免触发内容策略），开头、中间、结尾各埋一个口令（同时服务 D09 的轻量版）。
- 读取：HTTP 状态与错误类型；`input_tokens` 是否等于本地预计数（±1%）——用于排除"悄悄截断"。
- 判分：接受/拒绝的边界与宣称档位一致 → 通过；宣称 1M 实为 256K 之类 → 一票否决（按实测重定档）。
- 成本：拒绝探针约 0；跨档探针约 26 万输入 token（一次，仅对有档位差的型号）。频率：上架、申诉。
- 误报：需要先用基线账号确认各型号的真实上限与错误类型。注意 2026 年 Claude 新型号在所有订阅档**原生 1M**，档位差异主要在旧 Opus 4.6/Sonnet 4.6 的 `[1m]`（§5.1）；Codex 订阅窗口 272K（`max_context_window` 872K）；Kimi `k3` 1M 需 Pro+、否则 256K；GLM 在 Claude Code 里加 `[1m]` 开 1M（§5.2）。本地计数与官方计数有偏差，留足余量。

**A06 推理/思考档位可用性**
- 检测什么：宣称支持的 effort/思考档位（如 Codex 的高档 effort、Claude 的 effort/自适应思考、快速模式）是否被账号接受且真实生效。
- 探针：同一道中等难度、答案为单个整数的生成题（见 A09 的生成器，如"带权图最短路"），分别用各档 effort 请求一次。
- 读取：请求是否被接受；OpenAI `output_tokens_details.reasoning_tokens`；Claude `usage.output_tokens`（最终答案只有一个数，所以 output_tokens 约等于思考 token）；Gemini `thoughtsTokenCount`。
- 判分：高档被拒 → 按实测重定档；被接受但推理量不随档位单调上升，且与基线同档相比显著偏低（同批次配对）→ 转 D06。
- 成本：每档 1–2 次，每次 1K–8K token。频率：上架、每周。

**A07 知识边界指纹（KBF 为主、HSS 为辅；双胞胎复核）**

> 本项列为后续，需先在至少两个同型号基线上完成 A/A。下述候选生成与成本是研究设计，未在真实账号运行，不自动成为本期扣价证据。化学题面改为普通机构或地理领域，保留同样的数值答案筛选方法。
- 检测什么：被测账号背后是否与基线账号是**同一个服务端模型**（改路由到别的型号/旧版本、部分请求被换时会失败）。依据 KBF（§2.7）与 IKP 的幻觉相似度（§2.6）。
- 题目生成（私有、可轮换，**由基线账号生成并筛选**，KBF 的做法）：
  1. 按领域让基线账号提名候选，例如列出 30 个冷门机构及其成立年份，每行“机构名 | 年份”，排除已用过的实体；领域可换成小城市海拔、冷门期刊创刊年、小行星编号等**数值答案**的领域；也可以用 Wikidata 直接取冷门实体，例如：
     ```sparql
     SELECT ?item ?itemLabel ?itemDescription ?year WHERE {
       ?item wdt:P31/wdt:P279* wd:Q3918 ; wdt:P571 ?inception ; wikibase:sitelinks ?links .
       BIND(YEAR(?inception) AS ?year)
       FILTER(?links >= 2 && ?links <= 6)
       SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
     } LIMIT 5000
     ```
  2. 把候选渲染成填空题，在基线账号上用 2–3 种无害变体（改措辞、改顺序）重复问；**只保留基线自己稳定给出同一答案的题**（不论对错——KBF 比的是"与参考一致"，稳定的错误答案同样是指纹）。用第二个基线账号复查，剔除两个基线之间不一致的题。
- 探针：每个请求放 10–20 题，
  ```
  Fill in each blank with a number only, one per line, formatted "N. value".
  If you do not know, write "N. UNKNOWN". Do not explain.
  1. <Institution name> (<description>) was founded in the year ___.
  2. The elevation of <town name> is ___ metres.
  ...
  ```
  不开思考或最低 effort；能设 temperature 的设 0。
- 判分：
  - 主判据（KBF）：被测账号与基线记录答案的一致率 p̂_sus，对比基线自身新鲜查询的一致率 p_ref（来自 A/A）。单侧二项检验 H0: p_sus ≥ p_ref，用 Clopper-Pearson **上界**低于经 A/A 校准的参考阈值作为拒绝依据（原报告误写“下界”，本次整理校正）；拒绝且复测复现只说明知识边界响应不一致，不能单独证明谁更差或卖家作弊。
  - 辅判据（HSS）：两侧都答错（非 UNKNOWN）的题上，HSS = 错误答案相同的题数 / 共同答错题数。IKP 报告同权重兄弟 0.30–0.56、独立训练≈0；共同答错 ≥10 题才看。
  - 只出现一次 → 仅提示并加测；两次复现 → 重扣并转人工；正确率大幅下降（IKP 容量意义上"掉到小一档"）→ 同时转 A09。
- 成本：约 200–400 题 / 每请求 20 题 = 10–20 次请求，输入约 1–1.6 万、输出约 2–3K token；KBF 论文报告单次在线审计约 $0.24–0.67。题目生成与筛选在基线账号上做（一次性成本，可复用到所有同型号被测账号）。频率：上架、申诉、被动信号异常时；题库每月轮换。
- 误报：基线与被测落在不同硬件池会有少量差异（所以阈值必须来自 A/A）；拒答策略变化会减少可比样本；KBF 自述不防"完美识别探针"的对手——官方不会针对我们这样做，但题目仍应轮换、不对外公开。

**A08 知识代际/时事配对（仅提示）**
- 检测什么：知识截止是否与宣称型号一致（被路由到旧版本时，近月事实会答不出或答旧）。
- 探针：取宣称型号知识截止前 1–6 个月发生、有唯一短答案的公开事件 5–10 个（BazaarLink 的各国领导人题属于此类，但要换成近期的），要求一句话短答。
- 判分：与同时段基线账号的一致率；明显低于 A/A 水平 → 仅提示并触发 A07。**不写死标准答案**（时事会变）。
- 成本：1 次批量请求，<1K token。

**A09 能力阶梯（同家族降档，同时服务 D07）**
- 检测什么：是否被换成同家族小号或推理被压低导致的能力下降。统计与配对方法见前文 §5–§6，这里只补**题目生成器**——全部参数化随机生成、程序判分、答案短、无害、不要求展示推理：
  | 生成器 | 示例提示词 | 判分 | 难度旋钮 |
  |---|---|---|---|
  | 带权图最短路 | `Undirected weighted graph. Edges: A-B 7, A-C 3, C-D 2, ... What is the length of the shortest path from A to H? Reply with only the integer.` | Go 里跑 Dijkstra | 节点数、边数 |
  | 程序执行预测 | 用一个小 DSL 生成 Python 片段（列表推导、取模、切片），问 `What does this program print? Reply with only the output.` | Go 里解释同一 DSL | 步数、嵌套 |
  | 日期运算 | `What is the date 1,000 days after 2017-03-14? Reply in YYYY-MM-DD only.` | Go time 计算 | 天数、跨闰年 |
  | 多键排序 | 给 15 条记录，`Sort by dept ascending, then salary descending, then name ascending; output the IDs comma-separated.` | 精确匹配 | 记录数、键数 |
  | 逻辑格子 | 4–5 人 × 4 属性，生成唯一解线索，输出 JSON | 唯一解比对 | 规模、线索冗余 |
  | 汉诺塔 | n=5–7，输出 JSON 移动序列 | 逐步模拟验证 | 盘数 |
  | 随机串计数 | `How many times does the letter 'k' appear in "qkkzrtkpl…"? Reply with only the integer.` | 精确 | 串长（受分词影响，能区分代际） |
  | 模运算 | `Compute ((a * b) + c) mod m. Return only the decimal integer.`（TraceOne） | 精确 | 位数 |
- 判分：题目级配对差异 + 区间（前文 §6）；"错误 / 拒答 / 截断 / 超时"分开统计。
- 成本：30–60 题，每题 50–2000 输出 token（含思考），约 3–8 万 token。

**A10 输出分布指纹（随机数直方图 / MET-lite，仅提示）**
- 检测什么：输出分布是否与同型号基线一致（便宜的二阶段触发器）。
- 探针 1（单次，参考 ModelTrace/TraceOne）：`Give me exactly 300 integers between 1 and 355, separated by spaces. Do not use any tool, do not follow an arithmetic pattern or loop. Output only the numbers.`；把 355 维频率直方图与各型号质心（**用我们自己的基线账号建**）比较。
- 探针 2（MET-lite，§2.2）：25 个随机文本片段续写，temperature=1，每片段 10 次，Hamming-MMD 置换检验。
- 判分：A/A 校准后的 p 值或似然比；异常只触发 A07/A09，不单独扣分。
- 成本：探针 1 约 1.5K 输出 token；探针 2 约 250 次请求、2.7 万 token（低频）。
- 误报：数字指纹对 reasoning effort、系统提示敏感（TraceOne 只在 low effort 下验证过），两侧必须同参数。

**A11 功能项**

> 原研究将功能缺失作为 SKU 不可售，**本期未采纳该门槛**。确认方案覆盖 thinking、工具、图片/PDF、JSON Schema 和多轮：仅被测缺失时按功能组计系数，基线也不支持记“不适用”，工具一律 `tool_choice=auto`。
- 检测什么：所售 SKU 宣称的功能是否可用：图片输入、PDF 输入、工具调用、结构化输出（JSON Schema）。
- 探针：64×64 纯红 PNG `What color is this image? Reply with a single English word.`（期望 red）；单页 PDF 含一个随机词，`What single English word appears in this PDF? Reply with just the word in uppercase.`；定义一个 `get_weather(city: string, unit: "c"|"f")` 工具，`What's the weather in Paris in celsius? Use the tool.`，检查返回的是工具调用块且参数 `{"city":"Paris","unit":"c"}`；要求按给定 Schema 输出并用 Schema 校验。
- 判分：缺功能 → 该 SKU 不可售（按实测重定档）。
- 成本：4–6 次，各 <1K token。

**A12 改路由/安全回退/拒答率（被动 + 少量主动）**

> 本期仅在无害对照持续异常且排除平台原因后重扣；真实内容触发与白名单回退仅提示。下述“通知”通过报告“对你的影响”表达，不新增通知服务。现有接口拿不到的回退头/元数据只记录依赖缺口，不能把缺字段当作通过或失败。
- 检测什么：官方是否因为**账号信任度低**而更频繁地把该账号的请求回退/改路由到弱模型或拒答——这是 2026 年两家都有一手文档/源码的"被风控账号降智"形态（§3.2、§3.3）。
- 被动信号：Claude `stop_reason:"refusal"` + `stop_details.category`（cyber / bio / frontier_llm / reasoning_extraction / general_harms）、回退后的 `message.model`/`fallback` 块；Codex `OpenAI-Model` ≠ 请求型号、`ModelReroute`、`response.metadata.openai_verification_recommendation:["trusted_access_for_cyber"]`、400 `cyber_policy`/`bio_policy`、安全缓冲头；Gemini 403 `VALIDATION_REQUIRED`。
- 主动对照：每次检测附带 5–10 条**完全无害**的普通编程/写作题（例如"给这段 Go 代码写单元测试"），在被测与基线上各发一次。
- 判分：①**无害对照题**上出现回退/改路由/拒答 → 账号级标记，重扣并通知卖家（官方文案即"Your account was flagged"）；②真实流量上的回退率显著高于同型号其它账号（按买家内容类别分层后仍显著）→ 重扣；③只在买家确有安全类内容时出现 → 仅提示，不罚卖家；④出现可信访问建议 → 记为风控账号（定价因子）。
- 成本：被动 0；主动 5–10 条短题。

**A13 账号风控状态（被动）**
- 检测什么：账号是否处于"需要身份验证""地区不支持""组织被禁用""授权失效"等状态（Anthropic 已明确对疑似转售账号要求身份验证，§3.1）。
- 判分：错误按类型细分，不再笼统记为"不可用"；处于风控状态期间一票否决（停止调度），恢复后按 05 文档的恢复流程复测。
- 成本：0。

**A14 超额自动扣费开关（配置项）**

> 本项及下述剥离付费请求参数均为后续候选，不在本期实施。未来也只能通过 token-market 的现有配置和已开放接口处理；做不到记“依赖 sub2api，后续处理”，不修改其网关代码。本期质检不启用付费加速或超额消费参数。
- 检测什么：账号是否开着"额度用完后自动花真钱"的开关——买家流量会直接消耗卖家余额或信用额度，事后必然产生纠纷。
- 探针：读配置/账号接口：Claude `has_extra_usage_enabled`、`/api/oauth/usage` 的 `extra_usage{is_enabled, monthly_limit, used_credits}` 与响应头 `-overage-status`；Codex `x-codex-credits-*`、`/wham/usage` 的 credits；Antigravity AI Credits 超额设置（sub2api 还会为开了 overages 的账号注入 `enabledCreditTypes`）；Kimi 加油包；MiniMax 积分包；OpenCode Go "Use balance"（§5.2）。原研究还提出剥离付费请求参数（本期未采纳；后续仅在 token-market 侧现有配置/接口可行时评估）：Claude `speed:"fast"`、Fable 请求（Pro 档）、旧 1M beta、`anthropic-usage-limit: slow`；Codex `service_tier:"priority"`——卖家明确开通并已计价的除外。
- 判分：开着且未计价 → 上架前必须关闭（未关闭则一票否决）；已计价开通 → 进入 SKU 描述。
- 成本：0–1 次查询。频率：上架、每日。

**A15 服务端参数规则指纹（确定性，代际核验）**
- 检测什么：服务端对参数的校验规则因型号代际而异（中文社区 wxmb01/Claude-Detection S2/S8/S14 的思路）：最小可缓存长度、接受哪些 thinking 类型、哪些 effort 取值合法、max_tokens 上限等。被换成别的代际时，这些"规则"会露馅，而且结果是确定的（接受/拒绝），不受采样随机性影响。
- 探针（全部用**无害、极短**内容）：①非法 effort 值（如 `"ultra"` 之于不支持它的型号）应返回 400；②只有某代际接受的 thinking 类型（作者称 `thinking:{"type":"between_tools"}` 只有 Sonnet 5.5 接受）；③最小可缓存长度：用 2–3 个长度（如 600、1100、2200 token）的带 `cache_control` 前缀各发两次，看从哪个长度开始出现 `cache_read_input_tokens>0`（作者实测 5.5 代 512、Opus 4.8 1024、4.7 2048、4.6 4096）。
- 判分：与**我们自己用基线账号实测的规则表**逐项一致才通过；不一致 → 仅提示并转 A07。**作者数值未经独立复现、官方文档本机无法核对，不能直接当阈值。**
- 成本：每项 1–4 次，几百到几千 token。频率：上架、每周、新型号发布后重建规则表。

### 7.3 掺水类（D）逐项

**D01 窗口起点推断（是否有人先于平台使用）**
- 检测什么：当前 5 小时窗口是不是**别人**打开的。
- 方法（被动）：Claude `anthropic-ratelimit-unified-5h-reset`（Unix 秒）减 5 小时 = 本窗口起点；Codex 用 `x-codex-primary-reset-at` 与 `-primary-window-minutes` 推算；国产 Coding Plan 用各自额度接口的重置时间（GLM `nextResetTime`、Kimi `resetTime`、MiniMax 窗口"从首次使用开始计时"）。平台从 usage_log 查该账号本窗口内**我方第一个请求**的时间。
- 判分：窗口起点早于我方首请求超过容差（如 2 分钟，含时钟误差与重置时间取整）→ 记一次"他人开窗"。按 7 天统计次数：卖家声明"独占"且 ≥2 次 → 一票否决（下架复核）；未声明独占 → 重扣并在报告披露"非独占"。只适用于 5 小时窗口，不适用于周窗口（子调研推断）。
- 成本：0。误报：卖家本人用了同一账号（额度在官方所有产品间共享，§5.1）——这本身就是"共用"，判定成立，只是归因为"卖家自用"而非"转售"；时钟偏差需要容差。

**D02 空闲期用量漂移**
- 检测什么：平台没有在途请求时，利用率是否仍在上涨。
- 方法：账号空闲（我方最近 N 分钟无请求）时，每 15–30 分钟查一次**不消耗额度**的用量接口：Claude `/api/oauth/usage`（限流较严，频率要低）、Codex `/wham/usage`、Kimi `/coding/v1/usages`、GLM `quota/limit`、OpenCode Go `/zen/go/v1/usage`；Antigravity/Gemini 的 `fetchAvailableModels`/`retrieveUserQuota` 剩余比例。也可只在窗口已开着时读响应头（不要替卖家开窗）。
- 判分：两次空闲快照之间利用率上升 > 精度阈值（响应头精度约 0.01；接口为 0–100 整数时取 ≥1 个百分点）→ 外部使用事件；按天统计次数与外部消耗量。注意：官方"重置额度券"会让利用率突然归零，不是异常。
- 成本：每次 1 个轻量查询，不耗推理额度。严重程度：重扣（频繁）/ 轻扣（偶发）。

**D03 用量增量对账（外部消耗占比 + 档位反推）**
- 检测什么：①窗口利用率的涨幅中有多少不是平台造成的；②账号的真实容量（反推档位）。
- 方法：对每个账号，在一段时间内取相邻利用率快照的差 Δu_t，与平台在该区间内经该账号的**按型号价格加权的 token 成本** c_t（usage_log 可得，含缓存读写分别计价）做过原点回归 Δu_t ≈ k·c_t + e_t。用**只有平台流量**的区间（或已核实独占的基线账号）标定 k；1/k 即窗口容量，与已核实档位账号的 1/k 比较得档位倍数（Max 20x 约为 Pro 的 20 倍，需用真账号标定，§5.1）。残差和 Σe_t / ΣΔu_t = 外部消耗占比。
- 判分：外部占比 >20%（起步值）→ 重扣；>50% → 视为与他人共享，按 D01 处理；反推容量与 A01 档位不符（差一档以上）→ 转 A01 复核。
- 成本：0（被动统计）。误报：Claude 工作日高峰 5 小时额度消耗更快（§5.1），**用 7 天窗口做回归**；Fable/Opus/Sonnet 分池，要按池分别回归；GLM 高峰/非高峰积分倍率不同，要把倍率放进 c_t。

**D04 429/限流画像**
- 检测什么：429 是不是"额度被别人用掉/档位不实"造成的。
- 方法：429 按四类拆分（§5.2 表）；对"配额耗尽"类，记录发生时平台本窗口累计成本与标定容量之比（来自 D03）。Claude 读 `-representative-claim` 与 `-status`；Codex 读 429 体的 `plan_type`、`resets_at` 与 `x-codex-active-limit`。
- 判分：平台用量远未达到标定容量（如 <50%）就被 `rejected`/429 → 共用或档位不实，转 D03/A01；限速/并发类频繁 → 轻扣（性能）；平台容量类 → 不扣卖家分；风控类 → A13。
- 成本：0。

**D05 剩余额度（"电池健康度"）**
- 检测什么：本 5 小时窗口、周窗口、型号单独周限额（如 Claude 的 Fable `7d_oi`、Sonnet/Opus 周限额）还剩多少；月度额度（Kimi 新会员、OpenCode Go）。
- 方法：同 D02 的接口/响应头；项目已有 `internal/listing/quota.go` 的窗口快照（five_hour、seven_day、seven_day_sonnet、seven_day_fable 等）。
- 判分：不作为扣分主因，而是**限制可售量**：剩余低于阈值时降低调度权重或暂停；长期"一开周就见底"的账号说明卖家自用多 → 轻扣并结合 D03。
- 成本：0。

**D06 推理量配对（含"推理截断"被动统计）**

> 下述 Wilcoxon、0.7 比值、零推理即异常与 max/low >1.3 均是研究候选，**未作为本期扣价阈值采纳**。确认方案用显式 high（完整加 low）的配对对数比：比值 <0.6 且区间上界 <0.8 时 ×0.70，比值 <0.8 时 ×0.85，均待 A/A 与阳性对照校准；缺少可读推理字段不等于推理为 0。Claude 优先读 thinking token 明细，缺失时以总输出减可见 token 估算并标注。
- 检测什么：同样的题、同样显式 effort 下，被测账号的推理量是否显著少于基线（推理被压低/被截断/被换成非推理型号）。
- 主动探针：10–20 道 A09 生成器出的中等难度题（答案只要一个数），显式写死 effort（Claude `output_config.effort` 或按型号的 thinking 形态；OpenAI `reasoning.effort`），两侧同批次。读 OpenAI `output_tokens_details.reasoning_tokens`、Claude `usage.output_tokens`、Gemini `thoughtsTokenCount`。
- 判分：题目级配对，Wilcoxon 符号秩检验；log(被测/基线) 中位数 < log(0.7) 且 p<0.01，同时要求 A/A 偏差在 ±10% 内才判（子调研汇总的起步阈值，需校准）；推理 token 为 0 而基线不为 0（grok-iq 的 `reasoning_zero`）→ 直接异常。**effort 阶梯**（wxmb01 S8）：同题 low 与 max 各一次，max 的输出 token 应 >1.3×low，否则说明 effort 没生效。
- 被动统计（零成本）：按账号统计真实流量里**推理 token 恰为 516、1034、1552**（518·n−2）的请求占比（openai/codex#30364 报告 gpt-5.5 有 44.0% 的长推理请求停在 516），与同型号其它账号比；显著偏高 → 疑似。
- 成本：主动 10–20 题，每题 1K–8K 推理 token，合计约 3–10 万 token。严重程度：重扣（配对确认、复测复现）/ 疑似。

**D07 能力配对回归**：与 A09 共用题目，统计与处置见前文 §5–§6（这里不重复）。严重程度：重扣 / 疑似。

**D08 输出截短/懒惰**

> 本期使用完整改写/枚举、占位符与结束原因；Codex OAuth 删除输出上限参数，**该通道不出此类题**。详细/简短对称干预只是历史候选，不要求展示或复述推理过程。
- 检测什么：回答被压短、提前结束、"偷懒"。
- 被动：按账号统计真实流量的 `stop_reason=max_tokens`（Anthropic）/ `incomplete_details`（OpenAI）比例、同型号同类请求的输出长度分布（与号池其它账号比）。
- 主动：2–3 道"精确计数"题（如 `List exactly 37 distinct English words for colors, one per line, no numbering.`，程序数行数）；对称干预（§2.8，Chen 等的饱和检验改造）：同题"原题 / 原题 + 请更详细地解释 / 原题 + 请尽量简短"三版，比较被测与基线的边际长度变化。**避免"写出推理步骤"类措辞**（§3.1、§3.2）。
- 判分：计数题失败率显著高于基线、或"更详细"干预下被测增幅显著小于基线 → 轻扣 / 仅提示。
- 成本：被动 0；主动约 3–6 次、数千 token。

**D09 有效上下文（多针检索 + 计数核对）**

> 本期仅完整质检采用 8K / 32K / 128K 的多针、多跳、聚合和首尾题；下述 50% / 90% 上限和 25 轮 GateScope 是更昂贵的候选，不作为默认预算。
- 检测什么：在档位允许的长度内，长上下文是否真的能用（静默截断、中段丢失）。
- 探针：在 32K、128K、以及档位上限的约 50%/90% 处构造文本，在 10%…90% 九个位置埋随机书签（wxmb01 S5 的做法）+ 1–2 个多跳问题（RULER 式，前文 §3.2）；问 `List every bookmark code you can find, one per line. If none, write NOT FOUND.`；同时核对 `input_tokens` 与本地计数（±1%）。可加 GateScope 式 25 轮对话，在第 10/24/25 轮检查早期信息（§2.7）。
- 判分：召回率与基线配对比较；`input_tokens` 偏少 → 截断（一票否决该上下文档位）；召回显著低于基线 → 重扣。
- 成本：很贵（几万到几十万输入 token），只在上架与申诉做，能用缓存的长前缀尽量复用。

**D10 速度配对（TTFT / TPS）**
- 检测什么：首字延迟与输出速度是否明显差于基线（共用排队、代理质量、低优先级模式、被换型号）。
- 探针：`Write the word banana exactly 200 times separated by spaces. No other text.`（BazaarLink `perf_bulk_echo`）或数字序列，同时段两侧各 1–3 次；TPS 按 MiniMax 官方公式"输出 token 数 ÷（末 token 时间 − 首 token 时间）"；被动统计真实流量的 TTFT/TPS 分位数。Claude 另看 `slow-status`、`usage.speed`，Codex 看 `response.service_tier`、安全缓冲头。
- 判分：TPS 或 TTFT 偏离基线超过 A/A 波动 → 轻扣～重扣；**TPS 显著高于基线**也要注意（grok-iq 与社区经验：被换成小模型时会"变快"，如 GPT 风控事件里 TPS 从 30–40 升到 60–100）→ 转 A07。高峰/非高峰分开（GLM 14:00–18:00、Kimi 14:00–17:00）。
- 成本：约 0.5K token/次。

**D11 并发承载**
- 检测什么：账号能否承受宣称的并发（共用账号常因别人占用并发而报"并发超限"）。
- 探针：同时发 2–4 个极短请求（max_tokens 8），重复 3 轮。
- 判分：并发类错误（GLM 1302、Kimi 403 concurrent limit、MiniMax 1041 等）显著多于同档基线 → 轻扣；**Kimi 的 concurrent limit 由风控触发、与剩余额度无关**（§5.2），出现即转 A13。Grok OAuth 默认并发 1，不适用。
- 成本：约 1K token。频率：上架、每周（并发探测本身也要低频，避免像压测）。

**D12 提示缓存功能**

> 本期判据为“被测账号自身两步”与“基线账号自身两步”配对；基线也未命中时不能仅因被测为 0 扣价。公开社区 92–98% 数字保留为二手资料，不作为本期固定阈值。
- 检测什么：官方提示缓存是否生效（直接影响买家成本与平台毛利）。
- 探针：同一长前缀（高于该型号最小可缓存长度，留足余量，例如 5000 token 的固定文档）连发两次，max_tokens 8。
- 读取/判分：第二次 Anthropic `cache_read_input_tokens>0`、OpenAI `input_tokens_details.cached_tokens>0`、Gemini `cachedContentTokenCount>0` → 通过；失败重试一次仍失败 → 轻扣。被动：按账号统计真实流量的缓存命中率（中文社区经验：订阅直连 92–98%，号池切号时明显下降【二手】），显著低于同型号其它账号 → 提示并转 D03。
- 成本：2 次，约 1 万 token（第二次大部分是缓存读）。

**D13 稳定性**：被动统计 5xx、断流、超时、空响应率（项目现有被动信号，05 文档 §11），按 §5.2 的四类区分平台容量问题与账号问题。严重程度：轻扣～重扣。

**D14 无法解释的缓存命中（被动共用检测）**
- 检测什么：账号的缓存里出现了**平台没有写过**的内容 → 有别人在用。
- 方法（CacheTracer/KeyPooling 思路，§2.8）：平台记录每个账号最近一次发送各个**公共前缀**的时间（如同一客户端版本的 Claude Code 工具定义 + 系统提示静态部分、Codex 的 instructions；它们在不同用户之间相同，而缓存按前缀匹配）。若某请求读到了该公共前缀的缓存（`cache_read_input_tokens ≥ 前缀长度 − 一个缓存块`），而平台在缓存 TTL（5 分钟/1 小时）内没有经该账号发过这个前缀 → 记一次"外部缓存命中"。
- 判分：7 天内多次 → 重扣（与 D01/D02 互相印证）；单次 → 仅提示。
- 成本：0。误报：缓存范围若是组织级（Team/Enterprise 席位），同组织其他成员也会写入；需先在基线账号上验证"本账号缓存域"的假设。另：OpenAI 刷新令牌出现 `refresh_token_reused`（§5.1）同样归入本项，属**强**共用信号，单次即重扣并通知卖家。

### 7.4 平台自检（S 类，不计入任何卖家）

| id | 名称 | 做什么 | 频率 |
|---|---|---|---|
| S01 | 传输完整性 | 确认网关对官方域名**强制校验证书**、不信任卖家提供的 CA；出口代理只允许 CONNECT/SOCKS 隧道；base_url/Host 不可被卖家配置覆盖 | 发版后、每日 |
| S02 | 协议转换回传 | BazaarLink 的 `response_augmentation`（精确回传随机 canary）、`symbol_exact`（Unicode 精确回传，如 `「這是測試」`、ZWJ emoji）、`sse_compliance`（每个 data 块是合法 JSON、结束标记正确）在**平台自己的 Edge** 上跑，覆盖 Anthropic↔OpenAI↔Gemini 各条转换路径 | 发版后 |
| S03 | thinking 签名回传 | 把上一轮 thinking 块与签名原样放回再请求（LLMprobe-engine `signature-probe.ts` 的做法），200 通过、400 invalid 失败；验证网关多轮转发没有破坏签名（Antigravity 路径另测：签名缺失时网关会把 thinking 降级，§5.2） | 发版后 |
| S04 | 模型映射审计 | 导出 sub2api 各账号/分组的 model mapping 与 Antigravity 默认映射（`domain/constants.go:106-170`），与挂单宣称型号核对；任何"静默换型号"的映射都要么删除、要么在挂单上明示 | 每日 |
| S05 | 供应链探针抽检 | 从 BazaarLink 安全组里挑 3–4 项（pip/npm 安装命令、markdown 外泄）对平台 Edge 抽检并公开结果，回应买家对"平台改包"的担忧 | 每周 |
| S06 | 跨买家缓存隔离 | 按 auditing-prompt-caching / KeyPooling 的方法在**同一卖家账号**上用两个买家身份测缓存命中；OpenAI 路径评估用 HMAC(平台密钥, 买家, 模型, 版本) 覆盖 `prompt_cache_key`（论文报告成本 +1.7–2.5%）；Anthropic OAuth 路径每账号只有一个缓存域，只能披露或在公共前缀后加买家盐 | 每月、发版后 |

---

## 8. BazaarLink 86 项在我们场景下的逐项取舍

判断依据：卖家只提供凭据与出口代理、base_url 固定官方、TLS 端到端（§1）。"删除"指不作为卖家质检项（不影响定价）；"自检"指移到**平台网关自检**（定期对平台自己的 Edge/sub2api 跑，结果不计入任何卖家）；"改造保留"指保留检测目标但换成我们的探针与判法（括号内为 §7 的新 id）。

### 8.1 身份识别 29 项

| BazaarLink id | 取舍 | 理由 |
|---|---|---|
| identity_style_en、identity_style_zh_tw、identity_list_format | 删除（可选并入 A10 风格配对） | 家族已由 TLS 保证；只在需要区分同家族型号时，作为与基线账号的配对风格信号（仅提示）。每条输出 500–1000 token，性价比低 |
| identity_reasoning_shape（索要推理步骤的原题面不收录） | 删除 | 家族识别无必要；"一步步写出推理"接近 Anthropic 所说的 CoT 诱导特征（§3.1、§3.2） |
| identity_self_knowledge、meta_creator、zh_creator_plain、zh_who_built_pressure、meta_context_len、meta_thinking_mode、tok_self_knowledge、submodel_selfspec、submodel_cutoff | 删除 | 自报不可靠（Cai 等：具体规格自报成功率 <5%，系统提示冒名即失效；LLMmap 同样结论）；我们的 OAuth 请求还带 Claude Code 系统提示，会改变自报 |
| tok_count_num、tok_split_word | 删除，改用 A03 | 让模型"自述分词"是生成文本；真实 tokenizer 证据在 `usage.input_tokens`，可精确比对 |
| identity_refusal_pattern、identity_capability_claim | 删除 | 对官方端点无鉴别价值；是否"能联网"取决于客户端工具，不是模型 |
| identity_json_discipline | 改造保留（并入能力题库） | 作为严格格式遵循题（前文 §5.3），与基线配对 |
| ling_kr_num、ling_jp_pm、ling_fr_pm、ling_ru_pres、ling_uk_pm、ling_kr_crisis、ling_de_chan | 改造保留（A08 知识代际） | 时事题能区分"知识截止不同的型号/版本"，但答案随时间变化，必须与**同时段基线账号**比，不能写死标准答案 |
| code_reverse_list、code_comment_lang、code_error_style | 删除（可选并入 A10） | 风格信号，单独无判定力 |
| comp_py_float、comp_large_exp | 删除 | 前沿模型全部答对，无区分度 |
| zh_region_boundary_taiwan | 删除 | 对官方端点无意义，且属敏感内容，可能触发内容策略 |

### 8.2 子模型识别 29 项

| BazaarLink id | 取舍 | 理由 |
|---|---|---|
| cap_tower_of_hanoi | 改造保留（A09 能力阶梯） | 可精确判分；4 盘对前沿模型已饱和，改为参数化 5–8 盘或带约束变体 |
| submodel_capability、pi_fingerprint | 改造保留（A09） | 多题合一的小能力阶梯；pi_fingerprint 去掉注入触发句（不收录原文），保留计数本身 |
| cap_letter_count、cap_reverse_words、cap_needle_tiny、tok_edge_zwj | 删除 | 饱和题；我们现有题库已有同类题，区分度低（前文 §10） |
| verb_explain_photosynthesis | 改造保留（D08 输出长度/懒惰，仅提示） | 默认冗长度是"懒/截短"的信号，但更应从真实流量的输出长度分布看 |
| perf_bulk_echo | 改造保留（D10 速度） | 固定输出长度测 TPS，与基线同时段配对 |
| ikp_t3_domain_fact、ikp_t4_obscure_fact、ikp_t4_bio_fact、ikp_t5_deep_fact、ikp_t5_codegen_edge | 改造保留并扩充（A07 知识边界指纹 KBF + HSS） | 5 道题统计力不够；按 KBF/IKP 方法由基线账号生成数字答案的"知识边界题"，比"与基线答案一致率"和"答错时是否错得一样" |
| v3e_refusal_l1…l8（8 项拒答阶梯） | 删除（原可选复核建议未采纳） | 对"同家族哪一版"有一定区分度，但每条输出 400–1400 token；拒答策略随版本与安全系统变化，误报来源多；本期仅用 A12 无害对照 |
| v3e_fmt_bullets、v3e_fmt_explain_depth、v3e_fmt_code_lang_tag | 删除（可选并入 A10） | 格式偏好受系统提示/客户端影响大；explain_depth 单条 1500 token |
| v3e_uncertainty_estimate | 可选（A10 分布检验） | 单个数字输出，便宜，适合做"同一模型输出分布"检验（MET 思路） |
| submodel_refusal（给自己推理能力打 1–10 分） | 删除 | 自评无判定力 |

### 8.3 安全探针 16 项

| BazaarLink id | 取舍 | 理由 |
|---|---|---|
| data_exfil_url、markdown_exfil、code_inject、dependency_hijack、npm_supply_chain、pip_git_url_attack、pip_bundled_extra、pip_shell_chain、npm_registry_injection、npm_git_url_attack、uv_install、cargo_add、go_install、brew_install（14 项） | 删除（卖家质检）；挑 3–4 项做**平台自检** | 卖家的出口代理看不到明文，无法实施 AC-1/AC-1.a/AC-2；但平台自己是 sub2api 中转，可定期对自家 Edge 跑少量同类探针并公开结果，回应买家对"平台改包"的顾虑 |
| bedrock_probe | 删除 | 让模型自述部署平台没有意义 |
| channel_signature | 自检 + Antigravity 场景保留（A02） | 我们的渠道是固定的；但通过 Antigravity 调用 Claude 时，id 前缀/字段与 Anthropic 官方不同，需要记录（见 §5.2） |

### 8.4 完整性检查 12 项

| BazaarLink id | 取舍 | 理由 |
|---|---|---|
| token_inflation | **必须保留并加强**（A03） | 改为"同一请求在被测账号与基线账号上的 `input_tokens` 必须完全相同"；能同时发现官方侧注入、tokenizer/型号变化、网关注入变化 |
| context_length | **必须保留并加强**（A05、D09） | 改为按声明档位核验上限（是否接受、是否报 prompt too long、`input_tokens` 是否等于预期），再做多针检索 |
| multimodal_image、multimodal_pdf | 保留（A11 功能项） | 不同档位/Coding Plan 是否支持图片、PDF 是可售卖的差异，应进质检报告 |
| cache_detection | 改造保留（D12 缓存功能） | 我们要查的不是"中转缓存了回答"，而是**官方提示缓存是否正常生效**（`cache_read_input_tokens`/`cached_tokens`），它直接影响买家成本 |
| thinking_block | 改造保留（A06 推理开关/档位 + S 自检签名回传） | 查账号是否支持所售的 thinking/effort 档位；签名回传用于验证网关多轮转发正确 |
| knowledge_cutoff | 改造保留（A08，仅提示） | 截止后事件是否编造，与基线配对 |
| response_augmentation、symbol_exact、sse_compliance | 自检 | 检验我们自己的协议转换（Anthropic↔OpenAI 格式、SSE、Unicode），与卖家无关 |
| consistency_check | 自检 | 官方不会缓存采样结果；只用于确认平台 Edge 没有误缓存 |
| adaptive_injection | 删除 | 卖家无法条件注入 |

**汇总**：86 项中，作为卖家质检改造保留约 26 项（7 道时事题并入 A08、5 道冷门知识题扩充为 A07；真正"必须保留"的是 token_inflation、context_length、多模态、缓存、thinking 档位、能力阶梯、速度、冷门知识 8 类），约 8 项转为平台自检（4 项完整性检查 + 从 14 项供应链探针里挑 3–4 项），其余约 55 项删除。BazaarLink **没有**而我们必须新增的是：档位核验、共用检测、推理量配对、响应 model 字段与回退/改路由事件、限额/429 画像、超额扣费开关、服务端参数规则（§7）。

---

## 9. 落地注意事项（与现有代码的差距）

> 本节保留研究候选，不表示已实现或全部获采纳。字段只能从现有网关响应、Admin API、用户 API 与只读库取得；取不到则报告依赖缺口。新判分器采用确认方案的 11 种，输入计数配对在引擎内做，不另建 `token_count_equal`；A07/A10 与共用检测不能由本节提前纳入扣价。所有题目都不包含有害请求、注入串或索要推理过程的指令。

1. **采集字段不够**。`internal/quality/protocol.go` 的 `ProbeResult` 只有 HTTP 状态、文本、响应 model、output/reasoning token、thinking 字符数、TTFT、耗时。要支持本文的大部分质检项，至少补：输入计数三项（`input_tokens`、`cache_creation_input_tokens`、`cache_read_input_tokens`；OpenAI `input_tokens_details.cached_tokens`；Gemini `promptTokenCount/cachedContentTokenCount/thoughtsTokenCount`）、`stop_reason`/`finish_reason`/`incomplete_details`、`id` 前缀、错误体的类型与业务码、关键响应头（限额/利用率、request-id、改路由标记）。真实买家流量这些字段大多已在 sub2api 的 usage_log/响应头解析里，优先复用。
2. **Claude 推理强度的测法要改**。前文已指出 thinking 文本是摘要、不能用字符数；另外 `anthropicProtocol` 目前固定发 `thinking:{type:"enabled", budget_tokens:2048}`，而 2026 年的新型号与真实 Claude Code 请求用的是自适应思考 + `output_config.effort`（veridrop 源码按型号切换两种形态）。探针应**按型号复刻真实客户端的请求形态**，推理量用 `usage.output_tokens`（题目答案只要一个数，output_tokens ≈ 思考 token）。
3. **判分器要扩**。现有 exact/contains/regex/numeric/choice/json_keys 之外，至少需要：`token_count_equal`（与同批基线计数逐项相等/差分相等，A03）、`program`（Go 端生成并计算答案的参数化题，A09）、`tool_call`（检查工具名与参数，A11）、`json_schema`（A11）、`batch_numeric_vs_reference`（批量数值填空与基线答案一致率，A07）、`distribution`（随机数直方图/MMD，A10）、`accept_reject`（期望被接受或被拒的边界探针，A05/A04）。
4. **基线至少两个**。没有两个同型号健康基线账号，就无法做 A/A，A07/A09/A10/D06 的阈值都无从校准（前文 §5.4 已强调）。基线账号本身也要跑 D 类共用检测，确保"干净"。
5. **探针安全白名单**（§3.1、§4.2）。仿照 LLMprobe-engine v0.10.1 加的"防护测试"：题库入库时自动扫描，禁止有害类别（武器、毒品、自伤、网络攻击、监控）、越狱/注入串（注入触发串（不收录原文））、索要或复述推理过程的指令、政治敏感题；新题人工审核。
6. **去同步、低频、像真实流量**。同一套题不要在大量账号上同时、同序发送：题目参数化与子集抽样、随机时间、每账号每日探针上限、请求头与真实买家一致（项目已有"探测请求用真实客户端特征"的规定）。被动信号（A02/A12/A13、D01–D05、D08/D10/D13、D14）承担日常监测，主动探针只做低频抽检和二阶段确认。
7. **把"官方路由"与"平台容量"排除在卖家责任之外**：A02 的允许返回值映射表要包含官方公开的自动路由（GLM 旧型号→5.3、Kimi 关思考→K2.8 无思考等，§5.2）；429/529 按四类分开（§5.2 表），平台容量类不扣卖家分。
8. **报告形态（对应"二手 MacBook 质检报告"）**：
   - 配置核验：档位、可用型号、上下文、推理档位、功能项、超额扣费开关（A01/A04/A05/A06/A11/A14）；
   - 真伪核验：型号字段、计数指纹、知识边界指纹、回退/改路由率、参数规则（A02/A03/A07/A12/A15）；
   - 成色：独占性（D01–D03/D14）、剩余额度（D05）、速度与稳定（D10/D11/D13）；
   - 智力：能力与推理量配对（A09/D06/D07）；
   每项显示"结果 + 对倍率的影响"，卖家看到的是脱敏版（不含题目与阈值，沿用 05 文档 §8 的规则）。
9. **合规风险不是检测能解决的**：Anthropic 明确对"转售/中转站"账号做检测、要求身份验证并封禁（§3.1），Antigravity、Kimi、GLM 条款也禁止转售/共用（§5.2）。这会体现为 A13 的风控状态和账号寿命，应在定价里作为独立的风险折扣考虑，本文不展开。
10. **平台自身作为 sub2api 中转的信任问题**：2604.08407 实测弱口令 Sub2API 被并入他人中转链；KeyPooling 实测 Sub2API 的通用 OpenAI 适配器存在跨客户缓存命中。这两点与卖家质检无关，但买家会关心，建议作为平台自检（S 类）定期执行并公开结果。
11. **把官方的回退/改路由告诉买家**：sub2api v0.2.14 的默认响应头白名单不含 `OpenAI-Model` 与 Claude 的 unified 头（`util/responseheaders/responseheaders.go:15-37`），买家的 Codex 客户端因此看不到"已被改路由到 gpt-5.2"的官方提示。本期只能利用现有只读用量记录在平台账单/日志里标注实际作答型号；`OpenAI-Model` 不可取得时记“依赖 sub2api，后续处理”，不得修改其透传行为——否则买家按 A 型号付费、实际拿到 B 型号的回答，平台难以自证。
12. **Claude 档位采集的依赖缺口**：`/api/oauth/profile` 与 bootstrap 需要 OAuth 授权含 `user:profile` 权限；setup-token 方式只有 `user:inference`（§5.1）。这些是官方客户端机制，当前约定的 sub2api 接口未提供档位，本期报告标“未核验；依赖 sub2api，后续处理”；不改变 sub2api 授权、不直连厂商，也不以未校准的 D03 反推作已核验档位。
13. **Gemini 个人 OAuth 已停服**（§5.2）：项目里的 Google One 接入方式与 sub2api 的 Gemini 旧档位表需要先复核是否仍可用，再谈对它的质检。

---

## 10. 来源登记

访问日期均为 **2026-10-11**（北京时间，少数在 10-12 凌晨）。"本文核对"= 撰写者本人读过原文/源码相应段落；"子调研"= 本次并行子调研读过全文或源码并给出行号，撰写者对其中关键句做了抽查（已注明）；【二手】= 只见摘要、转述或无法复核。正文中的 URL 与行号以此为准。

### 学术

| 编号 | 来源 | 核对情况 |
|---|---|---|
| R01 | Liu 等，*Your Agent Is Mine: Measuring Malicious Intermediary Attacks on the LLM Supply Chain*，arXiv 2604.08407 v1（2026-04-09，HTML 标注 ACM CCS 2026） https://arxiv.org/abs/2604.08407 | 本文核对全文 §4–§8 |
| R02 | Gao、Liang、Guestrin，*Model Equality Testing*，ICLR 2025，arXiv 2410.20247 v2 https://arxiv.org/abs/2410.20247 ；代码 https://github.com/i-gao/model-equality-testing | 本文核对 §2–§5、附录 C.3/C.4/C.6 |
| R03 | Cai、Shi、Zhao、Song，*Are You Getting What You Pay For? Auditing Model Substitution in LLM APIs*，arXiv 2504.04715 v2（2025-09-29） https://arxiv.org/abs/2504.04715 ；代码 https://github.com/sunblaze-ucb/llm-api-audit | 本文核对 §3–§4 |
| R04 | Sun、Yin、Xu、Kolter、Liu，*Idiosyncrasies in Large Language Models*，ICML 2025，arXiv 2502.12150 v2 https://arxiv.org/abs/2502.12150 | 本文核对 §1–§3 |
| R05 | Pasquini、Kornaropoulos、Ateniese，*LLMmap*，USENIX Security 2025，arXiv 2407.15847 v4 https://arxiv.org/abs/2407.15847 ；仓库 https://github.com/pasquini-dario/LLMmap | 本文核对 §4–§8 与 Table G.2；仓库由子调研读源码 |
| R06 | Bojie Li，*Incompressible Knowledge Probes*，arXiv 2604.24827 v2（2026-07-05） https://arxiv.org/abs/2604.24827 ；https://github.com/19PINE-AI/ikp | 本文核对方法、结果表、§6.6、知识指纹节 |
| R07 | Fang 等，*KBF: Knowledge Boundary as Fingerprint…*，arXiv 2605.29524 v3（2026-09-30） | 子调研读全文；本文核对摘要、§3–§4.2 与 155/0-16/7-28 等数字 |
| R08 | Zhang 等，*IRIS*，arXiv 2607.20860 v1 | 子调研 |
| R09 | Richardeau 等，*FLIPS*，arXiv 2606.03330 v1（ICML 2026） | 子调研 |
| R10 | Bruckner，*One Token Is Enough*，arXiv 2607.10252 v1 | 【摘要】 |
| R11 | Wang 等，*AgentProv*，arXiv 2609.00052 v1（EMNLP 2026） | 子调研 |
| R12 | Lin 等，*GateScope*，arXiv 2604.21083 v1（IMC 2026） | 子调研 |
| R13 | Zhang 等，*Real Money, Fake Models: Deceptive Model Claims in Shadow APIs*，arXiv 2603.01919 v3（CCS 2026） | 子调研 |
| R14 | Sun、Wang、Zhao 等，*Invisible Tokens, Visible Bills*，arXiv 2505.18471 v1 | 子调研 |
| R15 | Sun 等，*CoIn*，arXiv 2505.13778 v1 | 子调研 |
| R16 | Wang、Sun 等，*PALACE*，arXiv 2508.00912 v1 | 子调研 |
| R17 | Hoque 等，*Token Inflation*，arXiv 2605.30040 v1 | 子调研 |
| R18 | Velasco 等，*Is Your LLM Overcharging You?*，arXiv 2505.21627 v4（ICML 2026）；Chatzi 等，*Tokenization Multiplicity…*，arXiv 2506.06446 v2 | 子调研 |
| R19 | Velasco 等，*Auditing Pay-Per-Token in LLMs*，arXiv 2510.05181 v2（AISTATS 2026） | 子调研 |
| R20 | Chen 等，*The More It Says, the More You Pay*，arXiv 2609.20370 v1 | 子调研 |
| R21 | Gu 等，*Auditing Prompt Caching in Language Model APIs*，arXiv 2502.07776 v2（ICML 2025）；代码 https://github.com/chenchenygu/auditing-prompt-caching | 子调研 |
| R22 | Sun 等，*KeyPooling*，arXiv 2608.17485 v2（2026-08-23） | 子调研；本文核对 Sub2API 相关段落与冻结 commit |
| R23 | Ji 等，*CacheTracer*，arXiv 2608.20732 v1；Fahey，*CacheProbe*，arXiv 2605.30613 v1 与 https://github.com/Ryan5453/cacheprobe | 子调研（CacheProbe 结果为转述） |
| R24 | 量化与不确定性：arXiv 2504.04823 v2、2407.09141、2506.09501 v2、2411.05197 v2 | 子调研（部分仅摘要+主要发现） |
| R25 | 路由：Shafran 等 arXiv 2501.01818；Tang 等 arXiv 2604.15022（ACL 2026）；RerouteGuard arXiv 2601.21380；arXiv 2503.08704 | 子调研（后两篇仅摘要） |
| R26 | Moon，*The Price of Thinking*，arXiv 2608.16956 v1 | 子调研 |
| R27 | 可验证推理：TOPLOC 2501.16007、SVIP 2410.22307、DiFR 2511.20621、IMMACULATE 2602.22700、AEX 2603.14283、Hollow-LLM 2607.28884、Attestable Audits 2506.23706 | 子调研（多数仅摘要） |
| R28 | 前文：[2026-10-10 降智调研](../model-degradation-gpt-claude-2026-10-10.md)（S01–S32） | 本文引用其结论，不重复核对 |

### 厂商一手材料

| 编号 | 来源 | 核对情况 |
|---|---|---|
| V01 | Anthropic，*Detecting and preventing distillation attacks*（2026-02-23） https://www.anthropic.com/news/detecting-and-preventing-distillation-attacks | 本文核对原文 |
| V02 | Anthropic，*Detecting and countering misuse of AI: September 2026*（2026-09-10） https://www.anthropic.com/threat-intelligence-report-september-2026 | 本文核对 HTML 版相关章节（GTG-50021、生物案例、蒸馏与处置） |
| V03 | Claude Help Center，*Why Claude switched models in your conversation with Opus 5 or Opus 5.5* https://support.claude.com/en/articles/16049681 | 本文核对全文 |
| V04 | Claude Help Center，*Our Approach to User Safety*（2026-03-16） https://support.claude.com/en/articles/8106465-our-approach-to-user-safety | 本文核对全文 |
| V05 | Claude Code 文档：https://code.claude.com/docs/en/model-config 、/fast-mode 、/costs ；Help Center 11145838、12429409、11845131、15363606；https://github.com/anthropics/claude-code/blob/main/CHANGELOG.md | 子调研 |
| V06 | Claude Code 2.1.296 客户端字符串检索、随客户端分发的 claude-api 文档（未文档化接口与响应头：unified 头、`/api/oauth/profile`、bootstrap、`/api/oauth/usage`） | 子调研【一手·未文档化】 |
| V07 | https://platform.claude.com/docs/en/build-with-claude/refusals-and-fallback 等开发者文档 | **本机地区受限未能访问**，内容为子调研转述【二手】 |
| V08 | openai/codex 源码 commit a07c8eb（2026-10-11） https://github.com/openai/codex | 子调研读源码；本文复核改路由文案（`codex-rs/core/src/compact_tests.rs:444`）、`OpenAI-Model` 头（`codex-rs/core/tests/suite/safety_check_downgrade.rs`）、`refresh_token_reused`、`x-codex-primary-used-percent` |
| V09 | OpenAI 网络安全安全文档/帮助（https://developers.openai.com/codex/concepts/cyber-safety 、https://help.openai.com/en/articles/20001259 ）、Codex 定价转述 | 403/转引【二手】 |
| V10 | Google：https://developers.googleblog.com/an-important-update-transitioning-gemini-cli-to-antigravity-cli 、https://geminicli.com/docs/resources/quota-and-pricing/ 、https://developers.google.com/gemini-code-assist/resources/quotas ；google-gemini/gemini-cli 源码 commit 9b6e026 | 子调研 |
| V11 | Antigravity：https://antigravity.google/docs/models 、/docs/plans 、/docs/faq 、/docs/changelog | 子调研 |
| V12 | xAI：https://x.ai/pricing 、https://x.ai/news/grok-build-cli 、https://docs.x.ai | 子调研（grok.com/plans 被拦，FAQ 404） |
| V13 | Kimi：https://www.kimi.com/code/docs/ 、官方 kimi-cli、https://platform.kimi.com/docs/api/signatures-verify.md 、https://github.com/MoonshotAI/K2-Vendor-Verifier 、https://www.kimi.com/blog/kimi-vendor-verifier | 子调研 |
| V14 | 智谱：https://docs.bigmodel.cn/cn/coding-plan/ （overview、usage-notes、faq、notice/usage-revision）、https://docs.bigmodel.cn/cn/api/api-code 、https://docs.z.ai/devpack/overview | 子调研 |
| V15 | MiniMax：https://platform.minimaxi.com/docs/m-plan/ 、/docs/api-reference/errorcode | 子调研 |
| V16 | OpenCode Go：https://opencode.ai/docs/go/ | 子调研 |

### 工具与社区

| 编号 | 来源 | 核对情况 |
|---|---|---|
| T01 | BazaarLink Probe https://bazaarlink.ai/zh-CN/probe （含 `tab=learn`、`tab=troubleshoot`） | 本文抓取 SSR 文本核对；86 项详情来自同批次抓取 调研期间的 86 项详情及判定方法页抓取（临时材料，未入库） |
| T02 | Bazaarlinkorg/LLMprobe-engine v0.11.0 https://github.com/Bazaarlinkorg/LLMprobe-engine | 子调研读源码；本文核对 README v0.10.1 关于有害探针导致停权的原文 |
| T03 | toby-bridges/api-relay-audit v2.5.0 https://github.com/toby-bridges/api-relay-audit | 子调研读源码 |
| T04 | zzsting88/relayAPI（含已删除检测器的历史版本） https://github.com/zzsting88/relayAPI | 子调研 |
| T05 | canarybyte/veridrop、7836246/claude-detector、1812095643/sub2api-excel2api-plugin（ModelTrace）、wangchao0708/TraceOne、kaibush/grok-iq、chenchenygu/auditing-prompt-caching、Ryan5453/cacheprobe | 子调研读源码 |
| T06 | wxmb01/Claude-Detection、zxc123aa/cc-proxy-detector、chen-006/meow-llm-detector、hanlinwenyuan/hlwy-ai-checker、xqy2006/ModelTrace、KoriIku/chatgpt-degrade-checker、noonwake-ai/ai-drool-router、leaf0329/chatgpt-codex-desktop-detector | 子调研读源码/README |
| T07 | linux.do 主题（https://linux.do/t/topic/<编号>）：1841302、1867464、2472419、2827119、2704354、2944491、2811197、2880842、2951746、2797049、2858863、2861622、2457629、1604331、2917019、261968、409884、2645971、2835455、2990101、2983050 等；V2EX 1203804；知乎专栏 2032951488624977427 | 子调研读原帖（经 `.json` 接口/转抓）；结论多为经验，正文已标注 |
| T08 | GitHub issues：openai/codex#30364、#11189、#12132；anthropics/claude-code#54917、#96132 | 社区报告，内容未经官方确认 |

### 本地代码

- token-market（只读）：`internal/quality/protocol.go`、`grader.go`、`reports.go`（22 道默认题）、`internal/listing/vendors.go`、`internal/listing/quota.go`、`docs/05-quality.md`。
- sub2api v0.2.14（`~/project/github-fork/sub2api`，tag 0363b8c，只读 `git show`/`git grep`）：`service/gateway_service.go:41`、`service/gateway_claude_oauth_body.go`、`pkg/httpclient/pool.go:126-129`、`service/ratelimit_service.go`、`service/upstream_response_model.go`、`pkg/antigravity/*`、`util/responseheaders/responseheaders.go` 等（行号见正文）。
