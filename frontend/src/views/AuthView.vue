<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'

import { useAuthStore } from '@/stores/auth'

type PanelName = 'login' | 'register'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const activePanel = ref<PanelName>('login')
const loginLoading = ref(false)
const registerLoading = ref(false)

const loginForm = reactive({
  username: authStore.lastUsername,
  password: '',
})

const registerForm = reactive({
  username: '',
  phone_number: '',
  password: '',
  confirmPassword: '',
})

const redirectPath = typeof route.query.redirect === 'string' ? route.query.redirect : '/dashboard'

async function submitLogin() {
  if (!loginForm.username.trim() || !loginForm.password.trim()) {
    ElMessage.warning('请填写用户名和密码')
    return
  }

  loginLoading.value = true
  try {
    await authStore.login({
      username: loginForm.username.trim(),
      password: loginForm.password,
    })
    ElMessage.success('登录成功，欢迎回来')
    await router.push(redirectPath)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '登录失败')
  } finally {
    loginLoading.value = false
  }
}

async function submitRegister() {
  if (!registerForm.username.trim() || !registerForm.phone_number.trim() || !registerForm.password.trim()) {
    ElMessage.warning('请先把注册信息填写完整')
    return
  }

  if (!/^1\d{10}$/.test(registerForm.phone_number.trim())) {
    ElMessage.warning('请输入正确的 11 位手机号')
    return
  }

  if (registerForm.password.length < 6) {
    ElMessage.warning('密码至少需要 6 位')
    return
  }

  if (registerForm.password !== registerForm.confirmPassword) {
    ElMessage.warning('两次输入的密码不一致')
    return
  }

  registerLoading.value = true
  try {
    await authStore.register({
      username: registerForm.username.trim(),
      phone_number: registerForm.phone_number.trim(),
      password: registerForm.password,
    })
    loginForm.username = registerForm.username.trim()
    loginForm.password = ''
    registerForm.password = ''
    registerForm.confirmPassword = ''
    activePanel.value = 'login'
    ElMessage.success('注册成功，请使用新账号登录')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '注册失败')
  } finally {
    registerLoading.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <div class="page-shell auth-layout">
      <section class="auth-brand glass-panel">
        <div class="auth-brand__badge">SmartFlow</div>
        <h1>把任务、课程与智能规划放在同一个工作台里。</h1>
        <p>
          这一版先把登录链路跑通。后面我们会在这个基础上继续接任务管理、课表总览和智能体排程能力。
        </p>

        <div class="auth-brand__points">
          <article>
            <strong>扁平化界面</strong>
            <span>去掉多余装饰，把信息层级讲清楚。</span>
          </article>
          <article>
            <strong>登录态托管</strong>
            <span>统一管理 access token，后续接业务页面更轻松。</span>
          </article>
          <article>
            <strong>可持续扩展</strong>
            <span>路由、状态、接口层已经拆开，后面直接加页面即可。</span>
          </article>
        </div>
      </section>

      <section class="auth-card glass-panel">
        <div class="auth-card__header">
          <div>
            <span class="auth-card__eyebrow">欢迎使用</span>
            <h2>账号入口</h2>
          </div>
          <p>先登录，再进入示例工作台。</p>
        </div>

        <el-tabs v-model="activePanel" stretch class="auth-tabs">
          <el-tab-pane label="登录" name="login">
            <el-form label-position="top" class="auth-form" @submit.prevent="submitLogin">
              <el-form-item label="用户名">
                <el-input
                  v-model="loginForm.username"
                  placeholder="请输入用户名"
                  size="large"
                  clearable
                />
              </el-form-item>

              <el-form-item label="密码">
                <el-input
                  v-model="loginForm.password"
                  type="password"
                  placeholder="请输入密码"
                  size="large"
                  show-password
                />
              </el-form-item>

              <el-button
                type="primary"
                size="large"
                class="auth-submit"
                :loading="loginLoading"
                @click="submitLogin"
              >
                登录并进入示例页
              </el-button>
            </el-form>
          </el-tab-pane>

          <el-tab-pane label="注册" name="register">
            <el-form label-position="top" class="auth-form" @submit.prevent="submitRegister">
              <el-form-item label="用户名">
                <el-input
                  v-model="registerForm.username"
                  placeholder="例如：losita"
                  size="large"
                  clearable
                />
              </el-form-item>

              <el-form-item label="手机号">
                <el-input
                  v-model="registerForm.phone_number"
                  placeholder="请输入 11 位手机号"
                  size="large"
                  clearable
                />
              </el-form-item>

              <el-form-item label="密码">
                <el-input
                  v-model="registerForm.password"
                  type="password"
                  placeholder="建议至少 6 位"
                  size="large"
                  show-password
                />
              </el-form-item>

              <el-form-item label="确认密码">
                <el-input
                  v-model="registerForm.confirmPassword"
                  type="password"
                  placeholder="请再次输入密码"
                  size="large"
                  show-password
                />
              </el-form-item>

              <el-button
                type="primary"
                size="large"
                class="auth-submit"
                :loading="registerLoading"
                @click="submitRegister"
              >
                创建账号
              </el-button>
            </el-form>
          </el-tab-pane>
        </el-tabs>
      </section>
    </div>
  </main>
</template>

<style scoped>
.auth-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  padding: 32px 0;
}

.auth-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.1fr) minmax(380px, 460px);
  gap: 24px;
  align-items: stretch;
}

.auth-brand,
.auth-card {
  border-radius: 28px;
}

.auth-brand {
  padding: 40px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  min-height: 680px;
}

.auth-brand__badge {
  width: fit-content;
  padding: 8px 14px;
  border-radius: 999px;
  background: #e8f2ff;
  color: #1f5fbf;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.auth-brand h1 {
  margin: 24px 0 16px;
  max-width: 10em;
  font-size: clamp(36px, 5vw, 56px);
  line-height: 1.08;
  letter-spacing: -0.04em;
  color: var(--text-main);
}

.auth-brand p {
  margin: 0;
  max-width: 38rem;
  color: var(--text-secondary);
  font-size: 16px;
}

.auth-brand__points {
  display: grid;
  gap: 14px;
  margin-top: 36px;
}

.auth-brand__points article {
  padding: 18px 20px;
  border-radius: 20px;
  background: rgba(255, 255, 255, 0.72);
  border: 1px solid rgba(17, 24, 39, 0.06);
}

.auth-brand__points strong {
  display: block;
  font-size: 16px;
  margin-bottom: 6px;
  color: var(--text-main);
}

.auth-brand__points span {
  color: var(--text-secondary);
  font-size: 14px;
}

.auth-card {
  padding: 30px 30px 24px;
  min-height: 680px;
}

.auth-card__header {
  margin-bottom: 18px;
}

.auth-card__header h2 {
  margin: 8px 0 8px;
  font-size: 30px;
  line-height: 1.15;
  letter-spacing: -0.03em;
}

.auth-card__header p {
  margin: 0;
  color: var(--text-secondary);
}

.auth-card__eyebrow {
  color: #1f5fbf;
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.auth-tabs {
  --el-color-primary: var(--brand);
}

.auth-form {
  margin-top: 16px;
}

.auth-submit {
  width: 100%;
  margin-top: 10px;
  height: 48px;
  border-radius: 14px;
  font-weight: 600;
  border: none;
  background: linear-gradient(180deg, var(--brand) 0%, var(--brand-strong) 100%);
}

@media (max-width: 1080px) {
  .auth-layout {
    grid-template-columns: 1fr;
  }

  .auth-brand,
  .auth-card {
    min-height: auto;
  }
}

@media (max-width: 640px) {
  .auth-page {
    padding: 16px 0;
  }

  .auth-brand,
  .auth-card {
    padding: 22px;
    border-radius: 22px;
  }
}
</style>
