# 前端设计系统与亮暗双主题

2026-07-28 重构。起因是界面沿用 Wails 模板 + 五份互不一致的手写 CSS，观感差。

## 为什么要有 token 层

重构前，`#4a7cff`、`#2b3242`、`#7c8798` 这几个值在 5 个组件里重复了几十遍，
button 与 input 的样式各写一份且取值不同（button padding 有 `6/13`、`8/15`、
`8/16`、`9/18` 四种；input 有 `6/9`、`8/10`、`7/11` 三种）。同屏出现两种高度的
控件就是这么来的——不是谁写错了，是没有单一来源。

现在 `src/styles/tokens.css` 是唯一色值出处，组件只引语义名。这条能自动验：

```bash
grep -rn '#[0-9a-fA-F]\{3,8\}\b' --include=*.svelte --include=*.css src \
  | grep -v 'styles/tokens.css'   # 应无输出
```

## 主题状态存三态，不存解析结果

`lib/theme.svelte.ts` 存的是 `'system' | 'light' | 'dark'`，不是解析后的
`light|dark`。**存解析结果会把「跟随系统」这层意图丢掉**——用户选了跟随系统，
之后系统切主题时界面不该纹丝不动。

配套的一点：`pref === 'system'` 时**移除** `<html data-theme>` 而非写入解析值，
交回 CSS 的 `prefers-color-scheme` 媒体查询。这样切系统主题不依赖 JS 也立刻生效，
且脚本执行前的首屏就有正确底色，不闪白。

代价是亮色取值要手抄两份（`[data-theme='light']` 和 `:root:not([data-theme])`）。
两份漂移了没人会立刻发现，所以 `preview/contrast.py` 里加了一致性校验。

## 对比度必须算，不能目测

改色时抓到两个目测不出来的问题：

| 问题 | 实测 | 修法 |
| --- | --- | --- |
| 主按钮白字压 `#4a7cff` | 3.74:1，不达 AA 的 4.5 | accent 压深到 `#2f5fe0`（5.48:1），hover 反而提亮到 `#4a7cff` |
| 亮色控件边框 `#ccd3e0` 压白底 | 1.50 | 提到 `#b3bdcd`（1.90） |

accent 的 hover 方向值得留意：**默认态比 hover 态更深**。看着反直觉，但正常态要承
白字所以必须深，hover 提亮才是「响应了」的正确方向感。

另一个坑在度量方式本身：判断卡片与页面底有没有层次，**不能用线性亮度的绝对差**。
相对亮度经过 gamma 曲线，同样「看起来差一档」在亮端的线性差值远大于暗端——用绝对差
会把暗色主题误判成一片平的（实测暗色差 0.0039、亮色差 0.0795，但换成对比度比值
是 1.068 vs 1.082，其实相当）。

## 侧栏 sticky 偏移依赖顶栏高度

`--h-topbar: 64px` 是个手工对齐的常量（品牌区两行文字 40px + 上下 padding 各 12px）。
侧栏 `top: calc(var(--h-topbar) + var(--sp-5))` 靠它避开顶栏。**改顶栏内边距或品牌
区行数时要同步改这个 token**，否则侧栏会被顶栏压住一截。

## preview/ 为什么必须存在

界面依赖 Wails 绑定（`window.go.main.App`），普通浏览器里打不开，改样式无法自查。
`preview/mock.ts` 桩掉绑定喂假数据，让页面能在 Chrome 里跑。

桩的时候踩了两个坑：

1. **`EventsOn` 是转调 `EventsOnMultiple` 实现的。** 只桩 `EventsOn` 会在订阅内核
   进度时抛 `TypeError` 并中断整个 mount，表现是页面渲染成空状态而非报错。
2. **两个独立的 `<script type="module">` 不保证执行顺序**（模块图并行拉取），
   mock 会晚于组件首次调用 API。得串成单入口里的静态 import + 动态 import
   （见 `preview/entry.ts`）。

`preview/` 不进生产包也不进类型检查：Vite 只以根 `index.html` 为入口，
`tsconfig.json` 的 `include` 只覆盖 `src/`。

## 遗留

`src/assets/fonts/` 下的 Nunito woff2 已无引用（它只含 latin 子集，中文全量回退
导致中英混排基线不齐，已换成系统字体栈）。文件本身没删。
