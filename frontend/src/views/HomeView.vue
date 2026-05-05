<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

// 导入资产
import heroImg from '@/assets/hero-dashboard.png'
import scheduleImg from '@/assets/feature-schedule.png'
import aiImg from '@/assets/feature-ai.png'
import toolsImg from '@/assets/feature-tools.png'

const router = useRouter()
const authStore = useAuthStore()

const ctaLink = computed(() => authStore.isAuthenticated ? '/dashboard' : '/auth')
const ctaText = computed(() => authStore.isAuthenticated ? '进入工作台' : '开始使用')

const handleCta = () => {
  router.push(ctaLink.value)
}

const features = [
  {
    title: 'AI 随口记',
    desc: '一句话记录作业、DDL、生活小事。AI 自动提取关键时间与内容。',
    img: toolsImg
  },
  {
    title: '四象限任务池',
    desc: '系统化承接日常待办，清晰标注优先级，告别混乱。',
    img: heroImg // 复用首页截图展示任务池部分，或在此处根据需要调整
  },
  {
    title: '课表智能编排',
    desc: '不仅仅是列出任务，而是智能寻找课间或空闲时段，把任务真正排进日程。',
    img: scheduleImg
  },
  {
    title: '长期记忆',
    desc: 'AI 会记住你的课程节奏、个人习惯、偏好和长期目标，越用越懂你。',
    img: aiImg
  }
]
</script>

<template>
  <div class="home-container">
    <!-- Navigation -->
    <nav class="home-nav">
      <div class="nav-content">
        <div class="brand">时伴 SmartMate</div>
        <button class="nav-cta" @click="handleCta">{{ ctaText }}</button>
      </div>
    </nav>

    <!-- Hero Section -->
    <header class="hero-section">
      <div class="hero-content">
        <p class="hero-eyebrow">成长型 AI 排程伙伴</p>
        <h1 class="hero-title gradient-text">越用越懂你的<br/>成长型 AI 排程伙伴</h1>
        <p class="hero-desc">
          不仅仅是待办清单。我们将课表、DDL、个人习惯与长期记忆深度融合，<br/>
          通过 AI 为你打造一张真正“能落地”的日程表。
        </p>
        <div class="hero-actions">
          <button class="primary-cta" @click="handleCta">{{ ctaText }}</button>
        </div>
      </div>
      <div class="hero-visual">
        <img :src="heroImg" alt="时伴工作台" class="main-screenshot" />
      </div>
    </header>

    <!-- Trust/Stats Section -->
    <section class="trust-section">
      <div class="section-container">
        <div class="stats-grid">
          <div class="stat-item">
            <span class="stat-value">12,000+</span>
            <span class="stat-label">活跃大学生用户</span>
          </div>
          <div class="stat-item">
            <span class="stat-value">850,000+</span>
            <span class="stat-label">AI 编排日程任务</span>
          </div>
          <div class="stat-item">
            <span class="stat-value">98.2%</span>
            <span class="stat-label">计划执行达成率</span>
          </div>
        </div>
      </div>
    </section>

    <!-- Problem vs Solution -->
    <section class="comparison-section">
      <div class="section-container">
        <div class="comparison-header">
          <h2 class="section-title">告别碎片化与混乱</h2>
          <p class="section-subtitle">为了解决大学生真实的计划难题而生</p>
        </div>
        <div class="comparison-grid">
          <div class="comparison-column problem">
            <h4>传统工具的困境</h4>
            <ul>
              <li>
                <strong>日历太“空”：</strong>
                只有格子，不知道课间 20 分钟能干什么。
              </li>
              <li>
                <strong>清单太“满”：</strong>
                列了一堆 DDL，却不知道该从哪一个开始。
              </li>
              <li>
                <strong>AI 太“乱”：</strong>
                直接改乱日程，让人感到失去掌控。
              </li>
            </ul>
          </div>
          <div class="comparison-column solution">
            <h4>时伴的解决之道</h4>
            <ul>
              <li>
                <strong>感知课表缝隙：</strong>
                自动识别课间与空课，填入最适合的任务。
              </li>
              <li>
                <strong>智能排序优先级：</strong>
                基于 DDL 与个人状态，自动给出当日最优解。
              </li>
              <li>
                <strong>预览确认机制：</strong>
                AI 负责提案，你负责最终决定，完美平衡。
              </li>
            </ul>
          </div>
        </div>
      </div>
    </section>

    <!-- Core Loop -->
    <section class="loop-section">
      <div class="section-container">
        <div class="loop-header">
          <h2 class="section-title">极简操作闭环</h2>
        </div>
        <div class="loop-grid">
          <div class="loop-item">
            <div class="loop-icon-box">💬</div>
            <span class="step-num">01</span>
            <strong>随口说需求</strong>
          </div>
          <div class="loop-arrow">→</div>
          <div class="loop-item">
            <div class="loop-icon-box">🧠</div>
            <span class="step-num">02</span>
            <strong>AI 识别任务</strong>
          </div>
          <div class="loop-arrow">→</div>
          <div class="loop-item">
            <div class="loop-icon-box">🗓️</div>
            <span class="step-num">03</span>
            <strong>基于课表编排</strong>
          </div>
          <div class="loop-arrow">→</div>
          <div class="loop-item">
            <div class="loop-icon-box">👀</div>
            <span class="step-num">04</span>
            <strong>预览微调</strong>
          </div>
          <div class="loop-arrow">→</div>
          <div class="loop-item">
            <div class="loop-icon-box">✅</div>
            <span class="step-num">05</span>
            <strong>确认应用</strong>
          </div>
        </div>
      </div>
    </section>

    <!-- Key Capabilities -->
    <section class="features-section">
      <div class="section-container">
        <h2 class="section-title">核心能力</h2>
        <div class="features-grid">
          <div v-for="feature in features" :key="feature.title" class="feature-item">
            <div class="feature-text">
              <h3>{{ feature.title }}</h3>
              <p>{{ feature.desc }}</p>
            </div>
            <div class="feature-media">
              <img :src="feature.img" :alt="feature.title" />
            </div>
          </div>
        </div>
      </div>
    </section>

    <!-- Final CTA -->
    <section class="final-cta-section">
      <div class="section-container">
        <div class="final-content">
          <h2 class="gradient-text">让计划不再停在待办列表里</h2>
          <p>加入数万名大学生，开启高效的智能校园生活。</p>
          <button class="primary-cta" @click="handleCta">{{ ctaText }}</button>
        </div>
      </div>
    </section>

    <!-- Footer -->
    <footer class="home-footer">
      <div class="footer-content">
        <p>© 2026 时伴 SmartMate · 大学生的智能排程伙伴</p>
      </div>
    </footer>
  </div>
</template>

<style scoped>
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
  font-size: 20px;
  font-weight: 800;
  color: #2563eb;
  letter-spacing: -0.02em;
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

/* Hero */
.hero-section {
  padding: 120px 0 80px;
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 80px;
}

.hero-content {
  max-width: 900px;
  padding: 0 24px;
}

.hero-eyebrow {
  font-size: 14px;
  font-weight: 700;
  color: #3b82f6;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin-bottom: 20px;
}

.hero-title {
  font-size: 72px;
  font-weight: 850;
  line-height: 1.05;
  letter-spacing: -0.04em;
  margin-bottom: 32px;
}

.hero-desc {
  font-size: 22px;
  color: #64748b;
  line-height: 1.6;
  margin-bottom: 48px;
}

.primary-cta {
  padding: 18px 48px;
  border-radius: 14px;
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

.hero-visual {
  width: 100%;
  max-width: 1100px;
  padding: 0 24px;
}

.main-screenshot {
  width: 100%;
  border-radius: 24px;
  box-shadow: 0 40px 80px -12px rgba(15, 23, 42, 0.18), 0 20px 40px -20px rgba(15, 23, 42, 0.12);
  border: 1px solid rgba(0, 0, 0, 0.05);
}

/* Stats */
.trust-section {
  padding: 60px 0;
  border-top: 1px solid #f1f5f9;
  border-bottom: 1px solid #f1f5f9;
}

.stats-grid {
  display: flex;
  justify-content: space-around;
  text-align: center;
}

.stat-item {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.stat-value {
  font-size: 32px;
  font-weight: 800;
  color: #0f172a;
}

.stat-label {
  font-size: 14px;
  color: #64748b;
  font-weight: 500;
}

/* Comparison */
.comparison-section {
  padding: 120px 0;
  background: #fcfdfe;
}

.comparison-header {
  text-align: center;
  margin-bottom: 80px;
}

.section-subtitle {
  font-size: 18px;
  color: #64748b;
  margin-top: 12px;
}

.comparison-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 60px;
}

.comparison-column {
  padding: 40px;
  border-radius: 24px;
  background: #fff;
  border: 1px solid rgba(0, 0, 0, 0.04);
}

.comparison-column h4 {
  font-size: 20px;
  font-weight: 800;
  margin-bottom: 24px;
}

.comparison-column.problem {
  border-left: 4px solid #cbd5e1;
}

.comparison-column.solution {
  border-left: 4px solid #3b82f6;
  background: linear-gradient(180deg, #f8faff 0%, #ffffff 100%);
}

.comparison-column ul {
  list-style: none;
  padding: 0;
  margin: 0;
}

.comparison-column li {
  margin-bottom: 20px;
  font-size: 16px;
  line-height: 1.6;
  color: #475569;
}

.comparison-column li strong {
  display: block;
  color: #1e293b;
  margin-bottom: 4px;
}

/* Loop */
.loop-section {
  padding: 100px 0;
  background: #f8fafc;
}

.loop-header {
  text-align: center;
  margin-bottom: 60px;
}

.loop-grid {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.loop-icon-box {
  width: 64px;
  height: 64px;
  border-radius: 18px;
  background: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 32px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.05);
  margin-bottom: 16px;
}

.loop-item {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
}

.step-num {
  font-size: 12px;
  font-weight: 800;
  color: #94a3b8;
  margin-bottom: 4px;
}

.loop-item strong {
  font-size: 16px;
  color: #334155;
}

.loop-arrow {
  color: #cbd5e1;
  font-size: 24px;
  margin-bottom: 40px;
}

/* Features */
.features-section {
  padding: 120px 0;
  background: #ffffff;
}

.section-title {
  font-size: 40px;
  font-weight: 850;
  text-align: center;
  margin-bottom: 80px;
  letter-spacing: -0.03em;
}

.features-grid {
  display: grid;
  gap: 140px;
}

.feature-item {
  display: grid;
  grid-template-columns: 1fr 1.5fr;
  gap: 80px;
  align-items: center;
}

.feature-item:nth-child(even) {
  grid-template-columns: 1.5fr 1fr;
}

.feature-item:nth-child(even) .feature-text {
  order: 2;
}

.feature-text h3 {
  font-size: 32px;
  font-weight: 800;
  margin-bottom: 24px;
  color: #0f172a;
}

.feature-text p {
  font-size: 18px;
  line-height: 1.7;
  color: #64748b;
}

.feature-media img {
  width: 100%;
  border-radius: 20px;
  box-shadow: 0 25px 50px rgba(0, 0, 0, 0.08);
  border: 1px solid rgba(0, 0, 0, 0.03);
}

/* Final CTA */
.final-cta-section {
  padding: 160px 0;
  text-align: center;
  background: linear-gradient(180deg, #ffffff 0%, #f8faff 100%);
}

.final-content h2 {
  font-size: 48px;
  font-weight: 850;
  margin-bottom: 24px;
}

.final-content p {
  font-size: 18px;
  color: #64748b;
  margin-bottom: 48px;
}

/* Footer */
.home-footer {
  padding: 60px 0;
  border-top: 1px solid #f1f5f9;
  text-align: center;
}

.footer-content p {
  font-size: 14px;
  color: #94a3b8;
}

/* Responsive */
@media (max-width: 1024px) {
  .hero-title { font-size: 56px; }
  .feature-item { grid-template-columns: 1fr !important; gap: 40px; text-align: center; }
  .feature-item .feature-text { order: 1 !important; }
  .feature-media { order: 2; }
  .comparison-grid { grid-template-columns: 1fr; gap: 32px; }
  .loop-grid { flex-wrap: wrap; justify-content: center; gap: 32px; }
  .loop-arrow { display: none; }
  .stats-grid { flex-wrap: wrap; gap: 40px; }
}

@media (max-width: 640px) {
  .hero-title { font-size: 40px; }
  .hero-desc { font-size: 18px; }
  .section-title { font-size: 32px; }
  .final-content h2 { font-size: 36px; }
}
</style>
