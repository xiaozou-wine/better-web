import './style.css'
import { mount } from 'svelte'
import App from './App.svelte'
import { applyTheme, theme } from './lib/theme.svelte'

// 挂载前先落主题，避免首帧用默认色渲染再跳到用户选择的主题。
applyTheme(theme.pref)

const target = document.getElementById('app')
if (!target) throw new Error('找不到挂载节点 #app')

// Svelte 5 用 mount 取代了 new Component({ target })。
const app = mount(App, { target })

export default app
