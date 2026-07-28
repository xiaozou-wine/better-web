/**
 * 预览入口。
 *
 * mock 必须在 main.ts 之前完成执行：两个独立的 <script type="module">
 * 不保证顺序（模块图并行拉取），因此串成显式的静态 import + 动态 import。
 */
import './mock'

await import('../src/main')
