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
        <div class="auth-brand__badge">SmartMate</div>
        <h1>您的全能智能排程伙伴。</h1>
        <p>
          SmartMate 将碎片化的任务管理、精准的课表同步与大模型驱动的智能规划深度融合，助您掌控每一刻，实现效率飞跃。
        </p>

        <div class="auth-brand__points">
          <article>
            <strong>智能代办规划</strong>
            <span>AI 助手自动分析任务优先级，平衡学业与生活，为您量身定制平衡的每日日程。</span>
          </article>
          <article>
            <strong>多源数据融合</strong>
            <span>无缝对接教务系统课表与个人待办清单，打破信息孤岛，实现真正的一站式时间管理。</span>
          </article>
          <article>
            <strong>极简设计哲学</strong>
            <span>采用现代建筑感扁平化设计，通过灵动的动效与极简层级，提升您的生产力与视觉愉悦感。</span>
          </article>
        </div>
      </section>

      <section class="auth-card glass-panel">
        <div class="auth-card__header">
          <span class="auth-card__eyebrow">SmartMate 智能日程</span>
          <h2>欢迎回来</h2>
          <p>请登录以同步您的学习与生活编排。</p>
        </div>

        <div class="auth-toggle">
          <button 
            type="button" 
            :class="['auth-toggle__btn', { active: activePanel === 'login' }]"
            @click="activePanel = 'login'"
          >
            登 录
          </button>
          <button 
            type="button" 
            :class="['auth-toggle__btn', { active: activePanel === 'register' }]"
            @click="activePanel = 'register'"
          >
            注 册
          </button>
          <div class="auth-toggle__slider" :style="{ transform: `translateX(${activePanel === 'login' ? '0' : '100%'})` }" />
        </div>

        <div class="auth-form-container">
          <Transition name="auth-fade" mode="out-in">
            <div v-if="activePanel === 'login'" key="login">
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
                登 录
              </el-button>
              </el-form>
            </div>

            <div v-else key="register">
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
          </div>
        </Transition>
      </div>
      </section>
    </div>
  </main>
</template>

<style scoped>
.auth-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 20px;
  background: #f8fafc;
  background-image: 
    radial-gradient(at 0% 0%, rgba(59, 130, 246, 0.05) 0px, transparent 50%),
    radial-gradient(at 100% 0%, rgba(96, 165, 250, 0.08) 0px, transparent 50%),
    radial-gradient(at 100% 100%, rgba(37, 99, 235, 0.05) 0px, transparent 50%),
    radial-gradient(at 0% 100%, rgba(59, 130, 246, 0.08) 0px, transparent 50%);
  position: relative;
  overflow: hidden;
}

.auth-page::before {
  content: "";
  position: absolute;
  top: -10%;
  left: -10%;
  width: 120%;
  height: 120%;
  background: url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noiseFilter'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.65' numOctaves='3' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noiseFilter)'/%3E%3C/svg%3E");
  opacity: 0.02;
  pointer-events: none;
}

.auth-layout {
  display: grid;
  grid-template-columns: 1fr 440px;
  gap: 40px;
  align-items: center;
  max-width: 1100px;
  width: 100%;
  z-index: 10;
}

.auth-brand {
  padding: 60px;
  background: transparent;
}

.auth-brand__badge {
  display: inline-block;
  padding: 6px 14px;
  background: #3b82f6;
  color: #ffffff;
  border-radius: 12px;
  font-size: 13px;
  font-weight: 800;
  letter-spacing: 0.04em;
  margin-bottom: 32px;
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.25);
}

.auth-brand h1 {
  font-size: clamp(40px, 4vw, 52px);
  font-weight: 900;
  color: #0f172a;
  line-height: 1.1;
  letter-spacing: -0.03em;
  margin-bottom: 24px;
}

.auth-brand p {
  font-size: 17px;
  color: #64748b;
  line-height: 1.6;
  margin-bottom: 48px;
  max-width: 480px;
}

.auth-brand__points {
  display: grid;
  gap: 20px;
}

.auth-brand__points article {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.auth-brand__points strong {
  font-size: 16px;
  font-weight: 800;
  color: #1e293b;
}

.auth-brand__points span {
  font-size: 14px;
  color: #64748b;
}

.auth-card {
  background: rgba(255, 255, 255, 0.8);
  backdrop-filter: blur(20px);
  border: 1px solid rgba(255, 255, 255, 0.6);
  border-radius: 32px;
  padding: 48px 40px;
  box-shadow: 
    0 4px 6px -1px rgba(0, 0, 0, 0.05),
    0 10px 15px -3px rgba(0, 0, 0, 0.1),
    0 20px 25px -5px rgba(0, 0, 0, 0.05);
}

.auth-card__header {
  margin-bottom: 32px;
}

.auth-card__header h2 {
  font-size: 28px;
  font-weight: 850;
  color: #0f172a;
  margin: 6px 0 10px;
}

.auth-card__header p {
  color: #64748b;
  font-size: 14px;
}

.auth-card__eyebrow {
  font-size: 12px;
  font-weight: 800;
  color: #3b82f6;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}

/* Custom Toggle Switch */
.auth-toggle {
  display: flex;
  background: #f1f5f9;
  border-radius: 14px;
  padding: 4px;
  position: relative;
  margin-bottom: 32px;
}

.auth-toggle__btn {
  flex: 1;
  border: none;
  background: transparent;
  padding: 10px 0;
  font-size: 14px;
  font-weight: 750;
  color: #64748b;
  cursor: pointer;
  z-index: 2;
  transition: color 0.3s;
}

.auth-toggle__btn.active {
  color: #0f172a;
}

.auth-toggle__slider {
  position: absolute;
  top: 4px;
  left: 4px;
  width: calc(50% - 4px);
  height: calc(100% - 8px);
  background: #ffffff;
  border-radius: 11px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
  transition: transform 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
  z-index: 1;
}

.auth-form {
  display: grid;
  gap: 16px;
}

:deep(.auth-form .el-form-item) {
  margin-bottom: 0;
}

:deep(.auth-form .el-form-item__label) {
  font-size: 13px !important;
  font-weight: 750 !important;
  color: #475569 !important;
  margin-bottom: 6px !important;
  line-height: 1 !important;
}

:deep(.auth-form .el-input__wrapper) {
  background: #f8fafc !important;
  border-radius: 12px !important;
  box-shadow: 0 0 0 1px #e2e8f0 inset !important;
  padding: 8px 16px !important;
  transition: all 0.2s;
}

:deep(.auth-form .el-input__wrapper.is-focus) {
  background: #ffffff !important;
  box-shadow: 0 0 0 2px #3b82f6 inset !important;
}

.auth-submit {
  width: 100%;
  height: 52px;
  margin-top: 12px;
  border: none;
  border-radius: 14px;
  background: #0f172a;
  color: #ffffff;
  font-size: 15px;
  font-weight: 750;
  cursor: pointer;
  transition: all 0.2s;
  box-shadow: 0 4px 12px rgba(15, 23, 42, 0.2);
}

.auth-submit:hover {
  background: #1e293b;
  transform: translateY(-1px);
  box-shadow: 0 6px 16px rgba(15, 23, 42, 0.3);
}

/* Transitions */
.auth-fade-enter-active,
.auth-fade-leave-active {
  transition: all 0.3s ease;
}

.auth-fade-enter-from {
  opacity: 0;
  transform: translateX(10px);
}

.auth-fade-leave-to {
  opacity: 0;
  transform: translateX(-10px);
}

@media (max-width: 1024px) {
  .auth-layout {
    grid-template-columns: 1fr;
    max-width: 500px;
  }
  
  .auth-brand {
    text-align: center;
    padding: 0 20px;
  }
  
  .auth-brand h1 {
    font-size: 36px;
  }
  
  .auth-brand p {
    margin: 0 auto 32px;
  }
  
  .auth-brand__points {
    display: none;
  }
}

@media (max-width: 480px) {
  .auth-card {
    padding: 32px 24px;
  }
}
</style>
