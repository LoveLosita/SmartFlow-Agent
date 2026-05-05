<script setup lang="ts">
import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { 
  Coin, 
  ShoppingCart, 
  Timer, 
  TrendCharts, 
  Wallet,
  Check,
  Refresh,
  InfoFilled
} from '@element-plus/icons-vue'

// --- 类型定义 ---
interface CreditSummary {
  recorded_credit_total: number
  applied_credit_total: number
  pending_apply_credit_total: number
  valid_until: string | null // 有效期至
  quota_sync_status: 'not_connected' | 'partial' | 'synced'
  tip: string
}

interface CreditProduct {
  product_id: number
  name: string
  description: string
  credit_amount: number
  price_cent: number
  price_text: string
  currency: 'CNY'
  badge: string
  status: 'active' | 'inactive'
}

interface CreditGrant {
  grant_id: number
  source_label: string
  amount: number
  status: 'recorded' | 'applied' | 'skipped' | 'failed'
  description: string
  created_at: string
}

// --- Mock 数据 ---
const summary = ref<CreditSummary>({
  recorded_credit_total: 120,
  applied_credit_total: 0,
  pending_apply_credit_total: 120,
  valid_until: "2026-06-05", // 默认显示一个月后
  quota_sync_status: 'not_connected',
  tip: '当前为 Credit 获取记录，后续会切换到 user/auth 权威额度。'
})

const products = ref<CreditProduct[]>([
  {
    product_id: 0,
    name: "Free",
    description: "每日免费发放，适合基础功能体验。",
    credit_amount: 100,
    price_cent: 0,
    price_text: "免费",
    currency: "CNY",
    badge: "每日",
    status: "active"
  },
  {
    product_id: 1,
    name: "Starter",
    description: "入门级额度，有效期 1 个月。续费时时间和额度均可累加。",
    credit_amount: 1000,
    price_cent: 990,
    price_text: "¥9.9",
    currency: "CNY",
    badge: "入门",
    status: "active"
  },
  {
    product_id: 2,
    name: "Lite",
    description: "经济型套餐，有效期 1 个月。适合日常轻度规划。",
    credit_amount: 3000,
    price_cent: 1990,
    price_text: "¥19.9",
    currency: "CNY",
    badge: "经济",
    status: "active"
  },
  {
    product_id: 3,
    name: "Pro",
    description: "专业版套餐，有效期 1 个月。最受深度规划用户欢迎。",
    credit_amount: 10000,
    price_cent: 3990,
    price_text: "¥39.9",
    currency: "CNY",
    badge: "Most Popular",
    status: "active"
  },
  {
    product_id: 4,
    name: "Max",
    description: "旗舰级套餐，有效期 1 个月。极致体验，额度充沛。",
    credit_amount: 40000,
    price_cent: 9990,
    price_text: "¥99.9",
    currency: "CNY",
    badge: "旗舰",
    status: "active"
  }
])

const grants = ref<CreditGrant[]>([
  {
    grant_id: 90002,
    source_label: "计划被点赞",
    amount: 1,
    status: "recorded",
    description: "你的计划《30 天高数强化复习计划》获得点赞",
    created_at: "2026-05-04T21:05:00+08:00"
  },
  {
    grant_id: 90001,
    source_label: "购买 Credit 包",
    amount: 1000,
    status: "recorded",
    description: "购买 Starter Credit 包",
    created_at: "2026-05-04T21:00:01+08:00"
  },
  {
    grant_id: 90000,
    source_label: "导入奖励",
    amount: 2,
    status: "recorded",
    description: "成功导入《雅思口语冲刺手册》",
    created_at: "2026-05-04T10:00:00+08:00"
  }
])

// --- 状态变量 ---
const isBuying = ref(false)
const historyLoading = ref(false)

// --- 方法 ---
async function handlePurchase(product: CreditProduct) {
  try {
    const isFree = product.price_cent === 0
    await ElMessageBox.confirm(
      isFree 
        ? `确定要领取每日免费的 ${product.credit_amount} Credit 吗？`
        : `确定要花费 ${product.price_text} 购买 ${product.credit_amount} Credit 吗？\n有效期一个月，续费可累加。`,
      isFree ? '领取确认' : '支付确认',
      {
        confirmButtonText: isFree ? '立即领取' : '确认支付',
        cancelButtonText: '取消',
        type: 'info',
        center: true
      }
    )
    
    isBuying.value = true
    // 模拟订单创建与支付流程
    await new Promise(r => setTimeout(r, 1500))
    
    // 更新本地状态
    summary.value.recorded_credit_total += product.credit_amount
    summary.value.pending_apply_credit_total += product.credit_amount
    
    const newGrant: CreditGrant = {
      grant_id: Date.now(),
      source_label: isFree ? "每日领取" : "购买 Credit 包",
      amount: product.credit_amount,
      status: "recorded",
      description: isFree ? "每日免费额度领取" : `购买${product.name}`,
      created_at: new Date().toISOString()
    }
    grants.value.unshift(newGrant)
    
    ElMessage({
      type: 'success',
      message: isFree ? `领取成功！已获得 ${product.credit_amount} Credit` : `支付成功！已充值 ${product.credit_amount} Credit`,
      duration: 3000
    })
  } catch {
    // 用户取消
  } finally {
    isBuying.value = false
  }
}

function formatDate(iso: string) {
  const date = new Date(iso)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

function refreshGrants() {
  historyLoading.value = true
  setTimeout(() => {
    historyLoading.value = false
    ElMessage.success('记录已更新')
  }, 800)
}
</script>

<template>
  <div class="store-container" v-loading="isBuying">
    <!-- Header -->
    <header class="store-header">
      <div class="header-left">
        <h1>Credit 商店</h1>
        <p>获取更多 Credit，解锁 AI 增强规划能力</p>
      </div>
      <div class="header-right">
        <el-button :icon="Refresh" circle @click="refreshGrants" />
      </div>
    </header>

    <!-- Balance Summary Card -->
    <div class="balance-card">
      <div class="balance-content">
        <div class="balance-main">
          <div class="label">累计获取 Credit</div>
          <div class="value">
            <el-icon><Coin /></el-icon>
            <span>{{ summary.recorded_credit_total }}</span>
          </div>
        </div>
        <div class="balance-details">
          <div class="detail-item" v-if="summary.valid_until">
            <el-icon><Timer /></el-icon>
            <span class="text">有效期至: {{ summary.valid_until }}</span>
          </div>
          <div class="detail-item">
            <span class="dot warning"></span>
            <span class="text">待同步: {{ summary.pending_apply_credit_total }}</span>
          </div>
          <div class="detail-item">
            <span class="dot success"></span>
            <span class="text">已同步: {{ summary.applied_credit_total }}</span>
          </div>
        </div>
      </div>
      <div class="balance-info">
        <el-alert
          :title="summary.tip"
          type="info"
          :closable="false"
          show-icon
        />
      </div>
      <!-- Background Ornament -->
      <div class="card-glow"></div>
    </div>

    <!-- Product Grid -->
    <section class="store-section">
      <h3 class="section-title"><el-icon><ShoppingCart /></el-icon> Credit 套餐</h3>
      <div class="product-grid">
        <div 
          v-for="product in products" 
          :key="product.product_id" 
          class="product-card"
          :class="{ 'is-popular': product.badge === 'Most Popular' }"
        >
          <div v-if="product.badge" class="product-badge">{{ product.badge }}</div>
          <div class="product-info">
            <h4 class="product-name">{{ product.name }}</h4>
            <p class="product-desc">{{ product.description }}</p>
          </div>
          <div class="product-amount">
            <el-icon><Coin /></el-icon>
            <span>{{ product.credit_amount }}</span>
          </div>
          <div class="product-footer">
            <div class="price">{{ product.price_text }}</div>
            <el-button 
              :type="product.badge === 'Most Popular' ? 'warning' : 'primary'" 
              round 
              @click="handlePurchase(product)"
            >
              {{ product.price_cent === 0 ? '立即领取' : '立即购买' }}
            </el-button>
          </div>
        </div>
      </div>
    </section>

    <!-- History -->
    <section class="store-section">
      <h3 class="section-title"><el-icon><TrendCharts /></el-icon> 获取记录</h3>
      <div class="history-list" v-loading="historyLoading">
        <div v-for="grant in grants" :key="grant.grant_id" class="history-item">
          <div class="history-icon" :class="grant.status">
            <el-icon v-if="grant.status === 'recorded'"><Check /></el-icon>
            <el-icon v-else><InfoFilled /></el-icon>
          </div>
          <div class="history-content">
            <div class="history-top">
              <span class="source">{{ grant.source_label }}</span>
              <span class="amount">+{{ grant.amount }} Credit</span>
            </div>
            <div class="history-bottom">
              <span class="desc">{{ grant.description }}</span>
              <span class="time">{{ formatDate(grant.created_at) }}</span>
            </div>
          </div>
        </div>
        
        <!-- Load More -->
        <div class="history-footer">
          <el-button link>查看更多记录 <el-icon><Timer /></el-icon></el-button>
        </div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.store-container {
  padding: 24px;
  height: 100%;
  overflow-y: auto;
  overflow-x: hidden; /* 强制禁用水平滚动 */
  display: flex;
  flex-direction: column;
  gap: 32px;
  background: #f8fafc;
  scrollbar-width: thin; /* Firefox */
  scrollbar-color: rgba(15, 23, 42, 0.1) transparent;
}

/* 自定义滚动条样式 */
.store-container::-webkit-scrollbar {
  width: 6px;
}

.store-container::-webkit-scrollbar-track {
  background: transparent;
}

.store-container::-webkit-scrollbar-thumb {
  background: rgba(15, 23, 42, 0.08);
  border-radius: 10px;
  transition: background 0.3s;
}

.store-container::-webkit-scrollbar-thumb:hover {
  background: rgba(15, 23, 42, 0.15);
}

/* Header */
.store-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.header-left h1 {
  font-size: 28px;
  font-weight: 800;
  margin: 0;
  background: linear-gradient(135deg, #0f172a 0%, #3b82f6 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.header-left p {
  color: #64748b;
  margin: 4px 0 0 0;
  font-size: 14px;
}

/* Balance Card */
.balance-card {
  position: relative;
  background: #0f172a;
  border-radius: 24px;
  padding: 32px;
  color: #fff;
  box-shadow: 0 20px 40px rgba(15, 23, 42, 0.15);
  display: flex;
  flex-direction: column;
  gap: 28px; /* 统一内部垂直间距 */
}

.balance-content {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  flex-wrap: wrap;
  gap: 20px;
}

.balance-main {
  /* 允许自然撑开 */
}

.balance-main .label {
  font-size: 14px;
  color: #94a3b8;
  font-weight: 500;
  margin-bottom: 12px;
  display: block;
}

.balance-main .value {
  font-size: 40px;
  font-weight: 800;
  display: flex;
  align-items: center;
  gap: 12px;
  color: #fff;
  line-height: 1.2;
  flex-wrap: wrap; /* 允许图标和数字在极窄屏下换行 */
}

.balance-main .value span {
  word-break: break-all;
}

.balance-info {
  /* 移除 margin-top，改由父容器 gap 控制 */
  width: 100%;
}

.balance-main .value .el-icon {
  color: #fbbf24;
  font-size: 0.8em; /* 随字号缩放 */
}

.balance-details {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.detail-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: #cbd5e1;
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.dot.warning { background: #fbbf24; }
.dot.success { background: #10b981; }

.balance-info {
  position: relative;
  z-index: 1;
}

.balance-info :deep(.el-alert) {
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: #94a3b8;
}

.card-glow {
  position: absolute;
  top: -50%;
  right: -10%;
  width: 300px;
  height: 300px;
  background: radial-gradient(circle, rgba(59, 130, 246, 0.2) 0%, transparent 70%);
  pointer-events: none;
}

/* Section */
.store-section {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 18px;
  font-weight: 700;
  color: #1e293b;
  margin: 0;
}

/* Product Grid */
.product-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 24px;
}

.product-card {
  background: #fff;
  border-radius: 24px;
  padding: 28px;
  border: 1px solid #e2e8f0;
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 20px;
  transition: all 0.4s cubic-bezier(0.16, 1, 0.3, 1);
  box-shadow: 0 4px 6px -1px rgba(0,0,0,0.05);
}

.product-card:hover {
  transform: translateY(-8px);
  box-shadow: 0 20px 30px -10px rgba(0, 0, 0, 0.15);
  border-color: #3b82f6;
}

.product-badge {
  position: absolute;
  top: 16px;
  right: 16px;
  background: #3b82f6;
  color: #fff;
  font-size: 11px;
  font-weight: 700;
  padding: 4px 12px;
  border-radius: 20px;
  text-transform: uppercase;
}

.product-card.is-popular {
  border-width: 2px;
  border-color: #f59e0b;
  background: linear-gradient(180deg, #fff 0%, #fffbeb 100%);
}

.product-card.is-popular .product-badge {
  background: #f59e0b;
  box-shadow: 0 4px 12px rgba(245, 158, 11, 0.3);
}

.product-name {
  font-size: 18px;
  font-weight: 700;
  margin: 0 0 8px 0;
  color: #1e293b;
}

.product-desc {
  font-size: 13px;
  color: #64748b;
  line-height: 1.5;
  margin: 0;
}

.product-amount {
  font-size: 32px;
  font-weight: 800;
  color: #1e293b;
  display: flex;
  align-items: center;
  gap: 8px;
}

.product-amount .el-icon {
  color: #fbbf24;
  font-size: 28px;
}

.product-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: auto;
  padding-top: 16px;
  border-top: 1px solid #f1f5f9;
}

.price {
  font-size: 20px;
  font-weight: 700;
  color: #1e293b;
}

/* History List */
.history-list {
  background: #fff;
  border-radius: 20px;
  border: 1px solid #e2e8f0;
  overflow: hidden;
}

.history-item {
  display: flex;
  gap: 16px;
  padding: 16px 24px;
  border-bottom: 1px solid #f1f5f9;
  transition: background 0.2s;
}

.history-item:hover {
  background: #f8fafc;
}

.history-item:last-child {
  border-bottom: none;
}

.history-icon {
  width: 40px;
  height: 40px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 18px;
}

.history-icon.recorded {
  background: rgba(16, 185, 129, 0.1);
  color: #10b981;
}

.history-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.history-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.history-top .source {
  font-weight: 700;
  color: #1e293b;
  font-size: 14px;
}

.history-top .amount {
  font-weight: 700;
  color: #10b981;
  font-size: 15px;
}

.history-bottom {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.history-bottom .desc {
  font-size: 13px;
  color: #64748b;
}

.history-bottom .time {
  font-size: 11px;
  color: #94a3b8;
}

.history-footer {
  padding: 12px;
  display: flex;
  justify-content: center;
  background: #fcfdfe;
}
</style>
