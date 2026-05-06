import type { GeeTestRegisterData, GeeTestValidateResult } from '@/types/api'

const GEETEST_SCRIPT_URL = 'https://static.geetest.com/static/tools/gt.js'

export interface GeeTestCaptchaInstance {
  appendTo(element: string | HTMLElement): void
  onReady(callback: () => void): void
  onSuccess(callback: () => void): void
  onError(callback: (error?: unknown) => void): void
  getValidate(): GeeTestValidateResult | undefined
  reset(): void
  destroy?: () => void
}

interface GeeTestInitOptions extends GeeTestRegisterData {
  product?: 'float' | 'popup' | 'custom'
  width?: string
  lang?: string
  https?: boolean
}

type InitGeeTest = (
  options: GeeTestInitOptions,
  callback: (captcha: GeeTestCaptchaInstance) => void,
) => void

declare global {
  interface Window {
    initGeetest?: InitGeeTest
  }
}

let scriptPromise: Promise<void> | null = null

// loadGeeTestScript 只负责把极验浏览器脚本加载进页面。
// 职责边界：
// 1. 负责去重加载，避免登录/注册切换时重复插入 script 标签；
// 2. 不负责初始化验证码实例，实例创建由 createGeeTestCaptcha 处理；
// 3. 若脚本拉取失败，直接抛错给上层 UI，由页面决定如何提示用户。
export async function loadGeeTestScript() {
  if (typeof window === 'undefined') {
    throw new Error('当前环境不支持加载极验脚本')
  }
  if (window.initGeetest) {
    return
  }

  if (!scriptPromise) {
    scriptPromise = new Promise<void>((resolve, reject) => {
      const script = document.createElement('script')
      script.src = GEETEST_SCRIPT_URL
      script.async = true
      script.defer = true
      script.dataset.geetest = 'true'
      script.onload = () => resolve()
      script.onerror = () => reject(new Error('极验脚本加载失败'))
      document.head.appendChild(script)
    }).catch((error) => {
      scriptPromise = null
      throw error
    })
  }

  await scriptPromise
  if (!window.initGeetest) {
    throw new Error('极验脚本加载完成，但初始化方法不可用')
  }
}

// createGeeTestCaptcha 负责把后端返回的 challenge 转成一个可挂载到 DOM 的验证码实例。
// 职责边界：
// 1. 只负责 `initGeetest` 这一层包装，不关心表单提交逻辑；
// 2. 默认使用 `float + 100%`，保证验证码块可以稳定嵌入登录/注册按钮上方；
// 3. 返回值只暴露极验实例本身，验证结果仍由页面在提交前主动读取。
export async function createGeeTestCaptcha(registerData: GeeTestRegisterData) {
  await loadGeeTestScript()

  return new Promise<GeeTestCaptchaInstance>((resolve, reject) => {
    const initGeetest = window.initGeetest
    if (!initGeetest) {
      reject(new Error('极验初始化方法不可用'))
      return
    }

    initGeetest(
      {
        ...registerData,
        product: 'float',
        width: '100%',
        https: true,
      },
      (captcha) => {
        resolve(captcha)
      },
    )
  })
}
