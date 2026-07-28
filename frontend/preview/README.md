# 前端视觉验证

不参与生产构建。Vite 只以根 `index.html` 为入口，`tsconfig.json` 的 `include`
只覆盖 `src/`，所以这里的代码既不进包也不进类型检查。

存在的原因：界面依赖 Wails 绑定（`window.go.main.App`），普通浏览器里打不开，
改样式无法自查。`mock.ts` 桩掉绑定并喂假数据，让页面能在 Chrome 里渲染。

## 用法

```bash
# 1. 起 dev server
npx vite --port 5199 --strictPort

# 2. 浏览器打开（?theme=light|dark 可指定主题）
#    http://localhost:5199/preview/index.html

# 3. 截图（需要本机装了 Chrome）
bash preview/shot.sh /tmp/bwshots
```

## 自动检查

三个脚本各查一层，都不需要目测：

| 脚本 | 查什么 |
| --- | --- |
| `contrast.py` | 解析 `tokens.css`，算各语义色对的 WCAG 对比度；并校验显式 `light` 与 `prefers-color-scheme` 亮色分支的取值一致 |
| `verify_shot.py` | 读截图像素，确认主题真的落到了渲染上、卡片与页面底有层次 |
| `interact.js` | 经 CDP 驱动页面，验证详情展开、主题三态循环、溢出菜单 |

```bash
python preview/contrast.py
python preview/verify_shot.py /tmp/bwshots

# interact.js 需要带远程调试端口的 Chrome
chrome --headless=new --remote-debugging-port=9333 \
  --user-data-dir=$(mktemp -d) \
  "http://localhost:5199/preview/index.html?theme=dark" &
node preview/interact.js
```

`contrast.py` 用比值而非亮度差判断层次：相对亮度经过 gamma 曲线，同样的
"看起来差一档"在亮端的线性差值远大于暗端，用绝对差会把暗色主题误判成没有层次。
