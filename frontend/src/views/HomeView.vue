<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const authStore = useAuthStore()

const ctaLink = computed(() => authStore.isAuthenticated ? '/dashboard' : '/auth')
const ctaText = computed(() => authStore.isAuthenticated ? '进入工作台' : '开始使用')

const handleCta = () => {
  router.push(ctaLink.value)
}

const logoImg = 'https://dl2.lecspace.com/SmartFlow-Agent/logo.png'

const landingImages = {
  homeOverview: 'https://dl2.lecspace.com/SmartFlow-Agent/%E4%B8%BB%E9%A1%B5.png',
  schedulePage: 'https://dl2.lecspace.com/SmartFlow-Agent/%E6%97%A5%E7%A8%8B%E9%A1%B5.png',
  quickNote: 'https://dl2.lecspace.com/SmartFlow-Agent/%E9%9A%8F%E5%8F%A3%E8%AE%B0.png',
  taskQuery: 'https://dl2.lecspace.com/SmartFlow-Agent/%E5%BE%85%E5%8A%9E%E6%9F%A5%E8%AF%A2.png',
  activeOptimize: 'https://dl2.lecspace.com/SmartFlow-Agent/%E4%B8%BB%E5%8A%A8%E4%BC%98%E5%8C%96.png',
  memorySystem: 'https://dl2.lecspace.com/SmartFlow-Agent/%E8%AE%B0%E5%BF%86%E7%B3%BB%E7%BB%9F.png',
}

const aiCapabilities = [
  {
    title: 'AI 随口记',
    desc: '用户只需要说“提醒我明天中午去菜鸟驿站取快递”，系统就能记录任务、识别截止时间，并自动归类。',
    img: landingImages.quickNote
  },
  {
    title: '自然语言待办查询',
    desc: '用户可以直接问“我明天有啥任务”，AI 会按条件查询并返回结构化任务卡片。',
    img: landingImages.taskQuery
  },
  {
    title: '主动排程优化',
    desc: '系统能分析当前排程节奏，发现连续高强度任务，并提出可确认的交换建议。',
    img: landingImages.activeOptimize
  },
  {
    title: '长期记忆',
    desc: '系统会逐渐理解用户关于日程、任务和学习习惯的偏好，让后续对话不用从零开始。',
    img: landingImages.memorySystem
  }
]
</script>

<template>
  <div class="home-container">
    <!-- Navigation -->
    <nav class="home-nav">
      <div class="nav-content">
        <div class="brand">
          <img :src="logoImg" alt="Logo" class="nav-logo" />
          <span>时伴 SmartMate</span>
        </div>
        <button class="nav-cta" @click="handleCta">{{ ctaText }}</button>
      </div>
    </nav>

    <!-- 一、首屏 Hero：项目一句话价值 -->
    <header class="hero-section">
      <div class="hero-content">
        <h1 class="hero-title gradient-text">从一句话，到一张可执行的日程表</h1>
        <p class="hero-desc">
          时伴 SmartMate 是面向大学生的 AI 排程伙伴，把课表、待办、AI 编排和长期记忆放进同一个日程系统，让计划真正落到每天能执行的时间里。
        </p>
        <div class="hero-tags">
          <span class="tag">AI 随口记</span>
          <span class="tag">课表智能编排</span>
          <span class="tag">预览确认执行</span>
          <span class="tag">长期记忆</span>
        </div>
        <div class="hero-actions">
          <button class="primary-cta" @click="handleCta">{{ ctaText }}</button>
          <a href="#core-loop" class="secondary-cta">查看核心闭环</a>
        </div>
      </div>
      <div class="hero-visual">
        <img :src="landingImages.homeOverview" alt="首屏总览" class="main-screenshot" />
      </div>
    </header>

    <!-- 二、完整性与价值：它解决什么问题 -->
    <section class="value-section">
      <div class="section-container">
        <div class="value-grid">
          <div class="value-content">
            <h2 class="section-title text-left">大学生的时间，不是一张空白日历</h2>
            <p class="section-desc">
              传统待办只能记录任务，普通日历不理解课程表，普通 AI 聊完也很难真正落地。时伴从学生真实课表出发，把作业、复习、实验、项目推进和临时小事统一放进可执行的安排里。
            </p>
            <ul class="value-points">
              <li>
                <div class="point-icon">📋</div>
                <div class="point-text">
                  <strong>任务不只是“要做什么”，还要决定“什么时候做”。</strong>
                </div>
              </li>
              <li>
                <div class="point-icon">🎓</div>
                <div class="point-text">
                  <strong>课表、水课、空档、DDL 都会影响安排结果。</strong>
                </div>
              </li>
              <li>
                <div class="point-icon">⚙️</div>
                <div class="point-text">
                  <strong>AI 生成建议后，用户可以预览、微调、确认，再正式应用。</strong>
                </div>
              </li>
            </ul>
          </div>
          <div class="value-visual">
            <img :src="landingImages.schedulePage" alt="日程页" class="shadow-img" />
          </div>
        </div>
      </div>
    </section>

    <!-- 三、完整闭环：从输入到执行 -->
    <section id="core-loop" class="loop-section">
      <div class="section-container">
        <div class="loop-header">
          <h2 class="section-title">一条能落地的 AI 排程闭环</h2>
          <p class="section-subtitle">AI 不是装饰，而是参与“理解、判断、编排、优化、记忆”的关键节点</p>
        </div>
        
        <div class="loop-timeline">
          <div class="timeline-step">
            <div class="step-content">
              <h3>1. 输入与理解</h3>
              <p>随口说需求 → AI 识别任务 → 判断截止时间与优先级 → 归入四象限任务池</p>
            </div>
            <div class="step-visual">
              <img :src="landingImages.quickNote" alt="输入与理解" class="shadow-img" />
            </div>
          </div>

          <div class="timeline-step reverse">
            <div class="step-content">
              <h3>2. 编排与确认</h3>
              <p>结合课表生成安排 → 预览微调 → 确认应用</p>
            </div>
            <div class="step-visual">
              <img :src="landingImages.schedulePage" alt="编排与确认" class="shadow-img" />
            </div>
          </div>

          <div class="timeline-step">
            <div class="step-content">
              <h3>3. 优化与沉淀</h3>
              <p>主动优化建议 → 记忆沉淀，越用越懂你</p>
            </div>
            <div class="step-visual">
              <img :src="landingImages.activeOptimize" alt="优化与沉淀" class="shadow-img" />
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- 四、AI 能力展示：AI 在其中起到什么关键作用 -->
    <section class="ai-capabilities-section">
      <div class="section-container">
        <h2 class="section-title">AI 不只聊天，而是进入任务执行链路</h2>
        <div class="capabilities-grid">
          <div v-for="cap in aiCapabilities" :key="cap.title" class="capability-card">
            <div class="cap-visual">
              <img :src="cap.img" :alt="cap.title" />
            </div>
            <div class="cap-content">
              <h3>{{ cap.title }}</h3>
              <p>{{ cap.desc }}</p>
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- 五、创新性：为什么它和普通工具不一样 -->
    <section class="innovation-section">
      <div class="section-container">
        <div class="innovation-header">
          <h2 class="section-title">不是待办工具，也不是普通聊天机器人</h2>
        </div>
        <div class="innovation-content">
          <div class="innovation-points">
            <div class="point-card">
              <div class="point-num">01</div>
              <h3>课表原生</h3>
              <p>以学生课程表为底座，而不是把大学生日程当成普通空白日历。</p>
            </div>
            <div class="point-card">
              <div class="point-num">02</div>
              <h3>确认式 AI 执行</h3>
              <p>AI 可以生成计划、查询任务、提出优化，但关键改动先预览、再确认，避免误操作。</p>
            </div>
            <div class="point-card">
              <div class="point-num">03</div>
              <h3>记忆驱动的个性化</h3>
              <p>系统能沉淀偏好和上下文，越用越懂用户的学习节奏。</p>
            </div>
          </div>
          <div class="innovation-visuals">
            <img :src="landingImages.activeOptimize" alt="主动排程优化" class="main-innovation-img shadow-img" />
            <img :src="landingImages.memorySystem" alt="记忆系统" class="sub-innovation-img shadow-img" />
          </div>
        </div>
      </div>
    </section>

    <!-- 六、技术实现性：方案为什么可信 -->
    <section class="tech-section">
      <div class="section-container">
        <div class="tech-grid">
          <div class="tech-content">
            <h2 class="section-title text-left">从前端工作台到 Agent 执行链路</h2>
            <p class="section-desc">
              时伴采用前后端分离架构，前端负责工作台交互和结果确认，后端通过 Agent Graph、工具调用、任务服务、课表服务和记忆服务完成理解、编排、查询与优化。AI 输出不会直接绕过业务规则，而是进入可追踪、可确认的执行链路。
            </p>
            <div class="tech-tags">
              <span class="tech-tag">Vue 前端工作台</span>
              <span class="tech-tag">API 网关</span>
              <span class="tech-tag">Agent Graph 分阶段执行</span>
              <span class="tech-tag">工具调用与结构化结果卡片</span>
              <span class="tech-tag">任务 / 课表 / 记忆服务</span>
              <span class="tech-tag">预览确认流</span>
              <span class="tech-tag">异步事件与长期记忆沉淀</span>
              <span class="tech-tag">可扩展至考研复习/项目推进</span>
            </div>
          </div>
          <div class="tech-visual">
            <img :src="landingImages.schedulePage" alt="架构与落地" class="shadow-img" />
          </div>
        </div>
      </div>
    </section>

    <!-- 七、Demo 稳定性与实际价值 -->
    <section class="demo-section">
      <div class="section-container">
        <div class="demo-header">
          <h2 class="section-title">当前 Demo 已能完整演示</h2>
          <p class="section-subtitle">从“回答问题”进入“辅助执行”，把模糊计划转成可执行日程</p>
        </div>
        <div class="demo-gallery">
          <div class="gallery-item"><img :src="landingImages.homeOverview" alt="首图" /></div>
          <div class="gallery-item"><img :src="landingImages.quickNote" alt="随口记" /></div>
          <div class="gallery-item"><img :src="landingImages.taskQuery" alt="待办查询" /></div>
          <div class="gallery-item"><img :src="landingImages.activeOptimize" alt="主动优化" /></div>
        </div>
        <div class="demo-values">
          <div class="value-badge">✔️ 减少手动拆任务、排时间的成本</div>
          <div class="value-badge">✔️ 降低 DDL 和临时事项遗漏风险</div>
        </div>
      </div>
    </section>

    <!-- 八、收尾 CTA -->
    <section class="final-cta-section">
      <div class="section-container">
        <div class="final-content">
          <h2 class="gradient-text">让计划不再停在待办列表里</h2>
          <p>时伴把任务理解、课表编排、主动优化和长期记忆接成一个闭环，让大学生日程管理真正可执行。</p>
          <button class="primary-cta" @click="handleCta">{{ ctaText }}</button>
        </div>
      </div>
    </section>

    <!-- Footer -->
    <footer class="home-footer">
      <div class="footer-content">
        <img :src="logoImg" alt="Logo" class="footer-logo" />
        <p>© 2026 时伴 SmartMate · 大学生的智能排程伙伴</p>
        <div class="footer-beian">
          <a href="https://beian.miit.gov.cn/" target="_blank" rel="noopener noreferrer">浙ICP备18041015号-4</a>
        </div>
      </div>
    </footer>
  </div>
</template>

<style scoped>
/* Basic Resets & Fonts */
.home-container {
  min-height: 100vh;
  background: #ffffff;
  color: #1e293b;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  overflow-x: hidden;
}

.section-container {
  max-width: 1200px;
  margin: 0 auto;
  padding: 0 24px;
}

/* Utils */
.gradient-text {
  background: linear-gradient(135deg, #0f172a 0%, #2563eb 50%, #3b82f6 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.text-left {
  text-align: left !important;
}

.shadow-img {
  width: 100%;
  border-radius: 16px;
  box-shadow: 0 20px 40px -12px rgba(15, 23, 42, 0.12), 0 10px 20px -10px rgba(15, 23, 42, 0.08);
  border: 1px solid rgba(0, 0, 0, 0.05);
}

/* Typography */
.section-title {
  font-size: 36px;
  font-weight: 850;
  text-align: center;
  margin-bottom: 24px;
  letter-spacing: -0.03em;
  color: #0f172a;
}

.section-subtitle {
  font-size: 18px;
  color: #64748b;
  text-align: center;
  margin-bottom: 48px;
  max-width: 800px;
  margin-left: auto;
  margin-right: auto;
}

.section-desc {
  font-size: 18px;
  color: #64748b;
  line-height: 1.6;
  margin-bottom: 32px;
}

/* Nav */
.home-nav {
  position: sticky;
  top: 0;
  z-index: 100;
  background: rgba(255, 255, 255, 0.8);
  backdrop-filter: blur(12px);
  border-bottom: 1px solid rgba(0, 0, 0, 0.05);
}

.nav-content {
  max-width: 1200px;
  margin: 0 auto;
  height: 72px;
  padding: 0 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 20px;
  font-weight: 800;
  color: #2563eb;
  letter-spacing: -0.02em;
}

.nav-logo {
  height: 32px;
  width: auto;
}

.nav-cta {
  padding: 8px 20px;
  border-radius: 99px;
  background: #1e293b;
  color: #fff;
  border: none;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.2s;
}

.nav-cta:hover {
  background: #0f172a;
  transform: translateY(-1px);
}

/* Hero Section */
.hero-section {
  padding: 100px 0 80px;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 60px;
  background: radial-gradient(circle at top center, #f0f7ff 0%, #ffffff 100%);
}

.hero-content {
  max-width: 900px;
  padding: 0 24px;
}

.hero-title {
  font-size: 64px;
  font-weight: 850;
  line-height: 1.1;
  letter-spacing: -0.04em;
  margin-bottom: 24px;
}

.hero-desc {
  font-size: 20px;
  color: #475569;
  line-height: 1.6;
  margin-bottom: 32px;
}

.hero-tags {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 12px;
  margin-bottom: 48px;
}

.tag {
  padding: 6px 16px;
  background: #e0f2fe;
  color: #0369a1;
  border-radius: 100px;
  font-size: 14px;
  font-weight: 600;
}

.hero-actions {
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 20px;
}

.primary-cta {
  padding: 16px 40px;
  border-radius: 12px;
  background: #2563eb;
  color: #fff;
  border: none;
  font-size: 18px;
  font-weight: 700;
  cursor: pointer;
  transition: all 0.2s;
  box-shadow: 0 10px 25px -5px rgba(37, 99, 235, 0.4);
}

.primary-cta:hover {
  background: #1d4ed8;
  transform: translateY(-2px);
  box-shadow: 0 15px 30px -5px rgba(37, 99, 235, 0.5);
}

.secondary-cta {
  font-size: 16px;
  font-weight: 600;
  color: #475569;
  text-decoration: none;
  transition: color 0.2s;
}

.secondary-cta:hover {
  color: #0f172a;
}

.hero-visual {
  width: 100%;
  max-width: 1100px;
  padding: 0 24px;
}

.main-screenshot {
  width: 100%;
  border-radius: 20px;
  box-shadow: 0 30px 60px -12px rgba(15, 23, 42, 0.15), 0 18px 36px -18px rgba(15, 23, 42, 0.1);
  border: 1px solid rgba(0, 0, 0, 0.05);
}

/* Value Section */
.value-section {
  padding: 100px 0;
  background: #fff;
}

.value-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 60px;
  align-items: center;
}

.value-points {
  list-style: none;
  padding: 0;
  margin: 0;
}

.value-points li {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 24px;
}

.point-icon {
  font-size: 24px;
  line-height: 1;
}

.point-text strong {
  display: block;
  font-size: 16px;
  color: #1e293b;
  margin-bottom: 4px;
  line-height: 1.5;
}

/* Loop Section (Timeline) */
.loop-section {
  padding: 100px 0;
  background: #f8fafc;
}

.loop-timeline {
  display: flex;
  flex-direction: column;
  gap: 60px;
  margin-top: 40px;
}

.timeline-step {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 60px;
  align-items: center;
}

.timeline-step.reverse .step-content {
  order: 2;
}

.step-content h3 {
  font-size: 28px;
  font-weight: 800;
  color: #0f172a;
  margin-bottom: 16px;
}

.step-content p {
  font-size: 18px;
  color: #64748b;
  line-height: 1.6;
}

/* AI Capabilities Section */
.ai-capabilities-section {
  padding: 120px 0;
  background: #ffffff;
}

.capabilities-grid {
  display: grid;
  grid-template-columns: repeat(2, 1fr);
  gap: 40px;
}

.capability-card {
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 20px;
  overflow: hidden;
  transition: transform 0.2s, box-shadow 0.2s;
}

.capability-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 10px 10px -5px rgba(0, 0, 0, 0.04);
}

.cap-visual img {
  width: 100%;
  height: auto;
  display: block;
  border-bottom: 1px solid #e2e8f0;
}

.cap-content {
  padding: 32px;
}

.cap-content h3 {
  font-size: 22px;
  font-weight: 800;
  color: #1e293b;
  margin-bottom: 12px;
}

.cap-content p {
  font-size: 16px;
  color: #64748b;
  line-height: 1.6;
}

/* Innovation Section */
.innovation-section {
  padding: 120px 0;
  background: #f1f5f9;
}

.innovation-content {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 60px;
  margin-top: 60px;
}

.innovation-points {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.point-card {
  background: #fff;
  padding: 32px;
  border-radius: 16px;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05);
}

.point-num {
  font-size: 32px;
  font-weight: 900;
  color: #cbd5e1;
  margin-bottom: 12px;
}

.point-card h3 {
  font-size: 20px;
  font-weight: 800;
  color: #0f172a;
  margin-bottom: 12px;
}

.point-card p {
  font-size: 16px;
  color: #64748b;
  line-height: 1.5;
}

.innovation-visuals {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
}

.main-innovation-img {
  width: 90%;
  z-index: 2;
}

.sub-innovation-img {
  position: absolute;
  bottom: -20px;
  right: -20px;
  width: 60%;
  z-index: 3;
  border: 4px solid #f1f5f9;
}

/* Tech Section */
.tech-section {
  padding: 120px 0;
  background: #fff;
}

.tech-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 60px;
  align-items: center;
}

.tech-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 24px;
}

.tech-tag {
  padding: 8px 16px;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  color: #334155;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
}

/* Demo Stability */
.demo-section {
  padding: 100px 0;
  background: #f8fafc;
}

.demo-gallery {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 20px;
  margin-bottom: 40px;
}

.gallery-item img {
  width: 100%;
  border-radius: 12px;
  box-shadow: 0 10px 15px -3px rgba(0, 0, 0, 0.1);
  transition: transform 0.3s;
}

.gallery-item img:hover {
  transform: scale(1.05);
}

.demo-values {
  display: flex;
  justify-content: center;
  gap: 24px;
  flex-wrap: wrap;
}

.value-badge {
  padding: 12px 24px;
  background: #fff;
  border-radius: 100px;
  font-size: 16px;
  font-weight: 600;
  color: #10b981;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.05);
}

/* Final CTA */
.final-cta-section {
  padding: 120px 0;
  text-align: center;
  background: linear-gradient(180deg, #ffffff 0%, #f0f7ff 100%);
}

.final-content h2 {
  font-size: 48px;
  font-weight: 850;
  margin-bottom: 24px;
}

.final-content p {
  font-size: 20px;
  color: #64748b;
  margin-bottom: 48px;
}

/* Footer */
.home-footer {
  padding: 40px 0;
  border-top: 1px solid #e2e8f0;
  text-align: center;
  background: #f8fafc;
}

.footer-logo {
  height: 24px;
  margin-bottom: 12px;
  opacity: 0.5;
  filter: grayscale(100%);
}

.footer-content p {
  font-size: 14px;
  color: #94a3b8;
}

.footer-beian {
  margin-top: 8px;
}

.footer-beian a {
  font-size: 12px;
  color: #94a3b8;
  text-decoration: none;
  transition: color 0.2s;
}

.footer-beian a:hover {
  color: #64748b;
}

/* Responsive */
@media (max-width: 1024px) {
  .hero-title { font-size: 48px; }
  .value-grid, .timeline-step, .innovation-content, .tech-grid {
    grid-template-columns: 1fr;
    gap: 40px;
  }
  .timeline-step.reverse .step-content { order: unset; }
  .capabilities-grid { grid-template-columns: 1fr; }
  .demo-gallery { grid-template-columns: repeat(2, 1fr); }
  .sub-innovation-img { position: relative; bottom: 0; right: 0; width: 100%; border: none; margin-top: 20px; }
  .main-innovation-img { width: 100%; }
}

@media (max-width: 640px) {
  .hero-title { font-size: 36px; }
  .hero-desc { font-size: 18px; }
  .section-title { font-size: 28px; }
  .final-content h2 { font-size: 32px; }
  .demo-gallery { grid-template-columns: 1fr; }
}
</style>
