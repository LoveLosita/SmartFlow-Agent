<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import http from '@/api/http'
import type { ApiResponse } from '@/types/api'
import {
  Check,
  Coin,
  Refresh,
  ShoppingCart,
  Timer,
  TrendCharts,
  Wallet,
} from '@element-plus/icons-vue'

type DashboardPeriod = '24h' | '7d' | '30d' | 'all'

interface ConsumptionDashboard {
  period: DashboardPeriod
  credit_consumed: number
  token_consumed: number
}

interface CreditSummary {
  current_credit_total: number
}

interface CreditProduct {
  product_id: number
  name: string
  description: string
  credit_amount: number
  price_cent: number
  original_price_cent?: number
  price_text: string
  currency: 'CNY'
  badge: string
  status: 'active' | 'inactive'
}

interface CreditGrant {
  grant_id: number
  source_label: string
  amount: number
  status: string
  description: string
  created_at: string
  direction: 'income' | 'expense'
  balance_after: number
  event_id: string
  order_id?: number
}

interface PageEnvelope<T> {
  items: T[]
  page: number
  page_size: number
  total: number
  has_more: boolean
}

interface CreditOrderResult {
  order_id: number
}

interface PeriodOption {
  value: DashboardPeriod
  label: string
}

// 1. 周期选项按需求固定为四档，避免前后端出现额外枚举不一致。
// 2. 文案直接使用页面展示文案，减少模板里重复判断。
const periodOptions: PeriodOption[] = [
  { value: '24h', label: '24h内' },
  { value: '7d', label: '7天内' },
  { value: '30d', label: '30天内' },
  { value: 'all', label: '全部' },
]

const selectedPeriod = ref<DashboardPeriod>('24h')
const dashboard = ref<ConsumptionDashboard>({
  period: '24h',
  credit_consumed: 0,
  token_consumed: 0,
})
const summary = ref<CreditSummary>({
  current_credit_total: 0,
})
const products = ref<CreditProduct[]>([])
const grants = ref<CreditGrant[]>([])

const isBuying = ref(false)
const dashboardLoading = ref(false)
const historyLoading = ref(false)
const hasMoreHistory = ref(false)
const historyPage = ref(1)
const initialized = ref(false)

const activePeriodLabel = computed(() => {
  return periodOptions.find((option) => option.value === selectedPeriod.value)?.label ?? '24h内'
})

/**
 * 负责加载顶部 Credit 消耗看板。
 * 不负责商品列表和流水列表，避免局部刷新时把整页耦合在一起。
 * 输入 period 为后端约定枚举；输出会同步当前看板数据与已选周期。
 */
async function loadDashboard(period: DashboardPeriod = selectedPeriod.value) {
  dashboardLoading.value = true
  try {
    const response = await http.get<ApiResponse<ConsumptionDashboard>>('/credit-store/consumption-dashboard', {
      params: { period },
    })
    const payload = response.data.data
    dashboard.value = {
      period: payload.period ?? period,
      credit_consumed: Number(payload.credit_consumed ?? 0),
      token_consumed: Number(payload.token_consumed ?? 0),
    }
    selectedPeriod.value = payload.period ?? period
  } finally {
    dashboardLoading.value = false
  }
}

/**
 * 负责加载 Credit 概览（如当前余额）。
 */
async function loadSummary() {
  try {
    const response = await http.get<ApiResponse<CreditSummary>>('/credit-store/summary')
    summary.value = response.data.data
  } catch (error) {
    console.error('加载 Credit 概览失败:', error)
  }
}

/**
 * 负责加载可购买套餐。
 * 不负责购买后的状态二次推断，页面仅消费后端返回的当前商品快照。
 */
async function loadProducts() {
  const response = await http.get<ApiResponse<{ items: CreditProduct[] }>>('/credit-store/products')
  products.value = response.data.data.items ?? []
}

/**
 * 负责加载流水列表。
 * 1. 首次加载或刷新时重置到第一页，保证刷新后看到最新记录。
 * 2. 加载更多时只在现有列表后追加，避免覆盖已经渲染的流水。
 * 3. 若请求失败，交给调用方提示，当前函数只负责维护列表状态。
 */
async function loadTransactions(append = false) {
  historyLoading.value = true
  try {
    const nextPage = append ? historyPage.value + 1 : 1
    const response = await http.get<ApiResponse<PageEnvelope<CreditGrant>>>('/credit-store/transactions', {
      params: {
        page: nextPage,
        page_size: 20,
      },
    })
    const payload = response.data.data
    if (append) {
      grants.value = [...grants.value, ...(payload.items ?? [])]
    } else {
      grants.value = payload.items ?? []
    }
    historyPage.value = payload.page || nextPage
    hasMoreHistory.value = payload.has_more
  } finally {
    historyLoading.value = false
  }
}

/**
 * 负责刷新“看板 + 流水”这两个联动区域。
 * 不刷新套餐区，避免每次点刷新都多打一类无关请求。
 */
async function refreshDashboardAndTransactions() {
  await Promise.all([loadDashboard(), loadSummary(), loadTransactions(false)])
}

/**
 * 负责初始化商店页。
 * 1. 首屏同时拉取看板、商品、流水，缩短整体等待时间。
 * 2. 只有三块数据都初始化完成后，才允许展示空状态文案，避免误判为空。
 */
async function loadStoreData() {
  await Promise.all([loadDashboard(), loadSummary(), loadProducts(), loadTransactions(false)])
  initialized.value = true
}

/**
 * 负责切换看板周期。
 * 不做本地聚合计算，始终以后端聚合结果为准，避免口径漂移。
 */
async function selectPeriod(period: DashboardPeriod) {
  if (period === selectedPeriod.value || dashboardLoading.value) {
    return
  }

  const previousPeriod = selectedPeriod.value
  selectedPeriod.value = period

  try {
    await loadDashboard(period)
  } catch (error) {
    selectedPeriod.value = previousPeriod
    ElMessage.error(error instanceof Error ? error.message : '切换消耗周期失败')
  }
}

/**
 * 负责购买套餐并在成功后刷新看板与流水。
 * 不负责推断支付渠道结果，当前仍沿用现有 mock-paid 流程。
 */
async function handlePurchase(product: CreditProduct) {
  try {
    const isFree = product.price_cent === 0
    await ElMessageBox.confirm(
      isFree
        ? `确定要领取每日免费的 ${product.credit_amount} Credit 吗？`
        : `确定要花费 ${product.price_text} 购买 ${product.credit_amount} Credit 吗？\n有效期一个月，续费可累计。`,
      isFree ? '领取确认' : '支付确认',
      {
        confirmButtonText: isFree ? '立即领取' : '确认支付',
        cancelButtonText: '取消',
        type: 'info',
        center: true,
      },
    )

    isBuying.value = true
    const createOrderResponse = await http.post<ApiResponse<CreditOrderResult>>(
      '/credit-store/orders',
      {
        product_id: product.product_id,
        quantity: 1,
      },
      {
        headers: {
          'X-Idempotency-Key': `credit-create-${product.product_id}-${Date.now()}`,
        },
      },
    )

    const orderID = createOrderResponse.data.data.order_id
    await http.post(
      `/credit-store/orders/${orderID}/mock-paid`,
      {
        mock_channel: 'mock',
      },
      {
        headers: {
          'X-Idempotency-Key': `credit-pay-${orderID}-${Date.now()}`,
        },
      },
    )

    // 1. 购买成功后同步刷新看板与流水。
    // 2. 套餐区保持现有数据源，不在这里额外触发商品重拉。
    await refreshDashboardAndTransactions()

    ElMessage({
      type: 'success',
      message: isFree
        ? `领取成功，已获得 ${product.credit_amount} Credit`
        : `支付成功，已充值 ${product.credit_amount} Credit`,
      duration: 3000,
    })
  } catch (error) {
    if (error === 'cancel' || error === 'close') {
      return
    }
    ElMessage.error(error instanceof Error ? error.message : '购买失败，请稍后重试')
  } finally {
    isBuying.value = false
  }
}

function formatMetricValue(value: number) {
  const normalizedValue = Number.isFinite(value) ? value : 0
  return normalizedValue.toLocaleString('zh-CN')
}

function formatOriginalPrice(cent: number) {
  if (!cent) return ''
  return `¥${(cent / 100).toLocaleString('zh-CN', { minimumFractionDigits: 0, maximumFractionDigits: 2 })}`
}

function formatAmount(grant: CreditGrant) {
  const prefix = grant.direction === 'expense' || grant.amount < 0 ? '' : '+'
  return `${prefix}${grant.amount} Credit`
}

function historyIconClass(grant: CreditGrant) {
  return grant.direction === 'expense' ? 'expense' : 'recorded'
}

async function loadMoreTransactions() {
  if (!hasMoreHistory.value || historyLoading.value) {
    return
  }
  try {
    await loadTransactions(true)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '加载更多流水失败')
  }
}

function formatDate(iso: string) {
  const date = new Date(iso)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

async function handleRefresh() {
  try {
    await refreshDashboardAndTransactions()
    ElMessage.success('看板和流水已刷新')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '刷新看板和流水失败')
  }
}

onMounted(async () => {
  try {
    await loadStoreData()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : 'Credit 商店加载失败')
  }
})
</script>

<template>
  <div class="store-container" v-loading="isBuying">
    <header class="store-header">
      <div class="header-left">
        <h1>Credit 商店</h1>
        <p>获取更多 Credit，解锁 AI 增强规划能力</p>
      </div>
      <div class="header-right">
        <el-button :icon="Refresh" circle :loading="dashboardLoading || historyLoading" @click="handleRefresh" />
      </div>
    </header>

    <div class="balance-card" v-loading="dashboardLoading">
      <div class="balance-content">
        <div class="balance-header">
          <div class="balance-title-wrap">
            <div class="balance-label-row">
              <span class="balance-tag">CONSUMPTION</span>
              <span class="balance-divider"></span>
              <span class="balance-period-text">{{ activePeriodLabel }}统计</span>
            </div>
            <h2 class="balance-main-title">消耗看板</h2>
          </div>
          
          <div class="period-switch-container">
            <div class="period-switch" role="tablist" aria-label="消耗周期">
              <button
                v-for="option in periodOptions"
                :key="option.value"
                type="button"
                class="period-chip"
                :class="{ active: option.value === selectedPeriod }"
                :disabled="dashboardLoading"
                @click="selectPeriod(option.value)"
              >
                {{ option.label }}
              </button>
            </div>
          </div>
        </div>

        <div class="balance-metrics">
          <div class="metric-card balance-metric">
            <div class="metric-icon-box">
              <el-icon><Wallet /></el-icon>
            </div>
            <div class="metric-details">
              <span class="metric-label">可用余额</span>
              <div class="metric-value-wrap">
                <span class="metric-value">{{ formatMetricValue(summary.current_credit_total) }}</span>
                <span class="metric-unit">Credits</span>
              </div>
            </div>
            <div class="metric-graph-hint">
              <div class="bar-bg"><div class="bar-fill" :style="{ width: '100%', opacity: 0.2 }"></div></div>
            </div>
          </div>

          <div class="metric-card credit-metric">
            <div class="metric-icon-box">
              <el-icon><Coin /></el-icon>
            </div>
            <div class="metric-details">
              <span class="metric-label">Credit 消耗</span>
              <div class="metric-value-wrap">
                <span class="metric-value">{{ formatMetricValue(dashboard.credit_consumed) }}</span>
                <span class="metric-unit">Credits</span>
              </div>
            </div>
            <div class="metric-graph-hint">
              <div class="bar-bg"><div class="bar-fill" :style="{ width: '65%' }"></div></div>
            </div>
          </div>

          <div class="metric-card token-metric">
            <div class="metric-icon-box">
              <el-icon><TrendCharts /></el-icon>
            </div>
            <div class="metric-details">
              <span class="metric-label">Token 消耗</span>
              <div class="metric-value-wrap">
                <span class="metric-value">{{ formatMetricValue(dashboard.token_consumed) }}</span>
                <span class="metric-unit">Tokens</span>
              </div>
            </div>
            <div class="metric-graph-hint">
              <div class="bar-bg"><div class="bar-fill" :style="{ width: '40%' }"></div></div>
            </div>
          </div>
        </div>
      </div>

      <!-- 装饰元素 -->
      <div class="balance-decorations">
        <div class="decor-circle decor-1"></div>
        <div class="decor-circle decor-2"></div>
        <div class="decor-mesh"></div>
      </div>
    </div>

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
            <div class="price-container">
              <div v-if="product.original_price_cent" class="original-price">
                {{ formatOriginalPrice(product.original_price_cent) }}
              </div>
              <div class="price">{{ product.price_text }}</div>
            </div>
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

    <section class="store-section">
      <h3 class="section-title"><el-icon><TrendCharts /></el-icon> 我的 Credit 流水</h3>
      <div class="history-list" v-loading="historyLoading">
        <div v-for="grant in grants" :key="grant.grant_id" class="history-item">
          <div class="history-icon" :class="historyIconClass(grant)">
            <el-icon v-if="grant.direction === 'expense'"><Wallet /></el-icon>
            <el-icon v-else><Check /></el-icon>
          </div>
          <div class="history-content">
            <div class="history-top">
              <span class="source">{{ grant.source_label }}</span>
              <span class="amount" :class="{ expense: grant.direction === 'expense' }">{{ formatAmount(grant) }}</span>
            </div>
            <div class="history-bottom">
              <span class="desc">{{ grant.description }}</span>
              <span class="time">{{ formatDate(grant.created_at) }}</span>
            </div>
          </div>
        </div>
        <div v-if="initialized && grants.length === 0" class="history-footer">
          <span class="empty-text">还没有 Credit 流水</span>
        </div>
        <div v-else class="history-footer">
          <el-button link :disabled="!hasMoreHistory" @click="loadMoreTransactions">
            {{ hasMoreHistory ? '查看更多记录' : '没有更多记录了' }} <el-icon><Timer /></el-icon>
          </el-button>
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
  overflow-x: hidden;
  display: flex;
  flex-direction: column;
  gap: 32px;
  background: #f8fafc;
  scrollbar-width: thin;
  scrollbar-color: rgba(15, 23, 42, 0.1) transparent;
}

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

.balance-card {
  position: relative;
  background: #0f172a;
  border-radius: 32px;
  padding: 40px;
  color: #fff;
  box-shadow: 0 25px 50px -12px rgba(15, 23, 42, 0.25);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.08);
  flex-shrink: 0;
  min-height: 420px;
}

.balance-content {
  position: relative;
  z-index: 2;
  display: flex;
  flex-direction: column;
  height: 100%;
}

.balance-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  margin-bottom: 40px;
  flex-wrap: wrap;
  gap: 24px;
}

.balance-label-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}

.balance-tag {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 1px;
  padding: 2px 8px;
  background: rgba(59, 130, 246, 0.2);
  color: #60a5fa;
  border-radius: 4px;
  text-transform: uppercase;
}

.balance-divider {
  width: 4px;
  height: 4px;
  background: rgba(255, 255, 255, 0.2);
  border-radius: 50%;
}

.balance-period-text {
  font-size: 13px;
  color: #94a3b8;
  font-weight: 500;
}

.balance-main-title {
  font-size: 32px;
  font-weight: 800;
  margin: 0;
  background: linear-gradient(to right, #fff, #94a3b8);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}

.period-switch {
  display: flex;
  background: rgba(255, 255, 255, 0.05);
  padding: 4px;
  border-radius: 14px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  backdrop-filter: blur(10px);
}

.period-chip {
  border: none;
  background: transparent;
  color: #94a3b8;
  padding: 8px 16px;
  border-radius: 10px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}

.period-chip:hover:not(:disabled) {
  color: #fff;
  background: rgba(255, 255, 255, 0.05);
}

.period-chip.active {
  background: #3b82f6;
  color: #fff;
  box-shadow: 0 4px 15px rgba(59, 130, 246, 0.4);
}

.balance-metrics {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 24px;
  flex: 1;
}

.metric-card {
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 24px;
  padding: 32px;
  display: flex;
  flex-direction: column;
  gap: 20px;
  transition: all 0.3s ease;
  position: relative;
  overflow: hidden;
}

.metric-card:hover {
  background: rgba(255, 255, 255, 0.06);
  border-color: rgba(255, 255, 255, 0.15);
  transform: translateY(-4px);
}

.metric-icon-box {
  width: 56px;
  height: 56px;
  border-radius: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 24px;
  margin-bottom: 4px;
}

.balance-metric .metric-icon-box {
  background: rgba(59, 130, 246, 0.15);
  color: #60a5fa;
  box-shadow: 0 8px 20px rgba(59, 130, 246, 0.15);
}

.credit-metric .metric-icon-box {
  background: rgba(245, 158, 11, 0.15);
  color: #fbbf24;
  box-shadow: 0 8px 20px rgba(245, 158, 11, 0.15);
}

.token-metric .metric-icon-box {
  background: rgba(16, 185, 129, 0.15);
  color: #10b981;
  box-shadow: 0 8px 20px rgba(16, 185, 129, 0.15);
}

.metric-label {
  font-size: 14px;
  color: #94a3b8;
  font-weight: 600;
  display: block;
  margin-bottom: 8px;
}

.metric-value-wrap {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.metric-value {
  font-size: 40px;
  font-weight: 800;
  color: #fff;
  letter-spacing: -1px;
}

.metric-unit {
  font-size: 14px;
  color: #64748b;
  font-weight: 500;
}

.metric-graph-hint {
  margin-top: auto;
}

.bar-bg {
  height: 6px;
  background: rgba(255, 255, 255, 0.05);
  border-radius: 3px;
  overflow: hidden;
}

.bar-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 1s ease-out;
}

.credit-metric .bar-fill {
  background: linear-gradient(to right, #f59e0b, #fbbf24);
  box-shadow: 0 0 10px rgba(245, 158, 11, 0.4);
}

.token-metric .bar-fill {
  background: linear-gradient(to right, #10b981, #34d399);
  box-shadow: 0 0 10px rgba(16, 185, 129, 0.4);
}

.balance-metric .bar-fill {
  background: linear-gradient(to right, #2563eb, #60a5fa);
  box-shadow: 0 0 10px rgba(37, 99, 235, 0.4);
}

.balance-decorations {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  pointer-events: none;
  z-index: 1;
}

.decor-circle {
  position: absolute;
  filter: blur(80px);
  border-radius: 50%;
  opacity: 0.15;
}

.decor-1 {
  top: -100px;
  right: -50px;
  width: 300px;
  height: 300px;
  background: #3b82f6;
}

.decor-2 {
  bottom: -50px;
  left: -50px;
  width: 250px;
  height: 250px;
  background: #8b5cf6;
}

.decor-mesh {
  position: absolute;
  inset: 0;
  background-image: radial-gradient(rgba(255, 255, 255, 0.03) 1px, transparent 1px);
  background-size: 32px 32px;
}

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

.product-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 28px;
}

.product-card {
  background: #fff;
  border-radius: 28px;
  padding: 32px;
  border: 1px solid #e2e8f0;
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 24px;
  transition: all 0.4s cubic-bezier(0.16, 1, 0.3, 1);
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.02), 0 2px 4px -1px rgba(0, 0, 0, 0.01);
}

.product-card:hover {
  transform: translateY(-10px);
  box-shadow: 0 30px 60px -12px rgba(15, 23, 42, 0.12);
  border-color: #3b82f6;
}

.product-badge {
  position: absolute;
  top: 20px;
  right: 20px;
  background: linear-gradient(135deg, #3b82f6 0%, #2563eb 100%);
  color: #fff;
  font-size: 10px;
  font-weight: 800;
  padding: 4px 12px;
  border-radius: 20px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.3);
}

.product-card.is-popular {
  border-width: 2px;
  border-color: #f59e0b;
  background: linear-gradient(180deg, #fff 0%, #fffbeb 100%);
}

.product-card.is-popular .product-badge {
  background: linear-gradient(135deg, #f59e0b 0%, #d97706 100%);
  box-shadow: 0 4px 12px rgba(245, 158, 11, 0.3);
}

.product-name {
  font-size: 20px;
  font-weight: 800;
  margin: 0 0 8px 0;
  color: #0f172a;
}

.product-desc {
  font-size: 14px;
  color: #64748b;
  line-height: 1.6;
  margin: 0;
}

.product-amount {
  font-size: 40px;
  font-weight: 800;
  color: #0f172a;
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 8px 0;
}

.product-amount .el-icon {
  color: #fbbf24;
  font-size: 32px;
  filter: drop-shadow(0 4px 8px rgba(251, 191, 36, 0.3));
}

.product-footer {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  margin-top: auto;
  padding-top: 24px;
  border-top: 1px solid #f1f5f9;
}

.price-container {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.original-price {
  font-size: 13px;
  color: #94a3b8;
  text-decoration: line-through;
  font-weight: 500;
  margin-bottom: -2px;
}

.price {
  font-size: 26px;
  font-weight: 800;
  color: #0f172a;
  letter-spacing: -0.5px;
  line-height: 1;
}

.history-list {
  background: #fff;
  border-radius: 28px;
  border: 1px solid #e2e8f0;
  overflow: hidden;
  box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.02);
}

.history-item {
  display: flex;
  gap: 20px;
  padding: 24px 32px;
  border-bottom: 1px solid #f8fafc;
  transition: all 0.2s;
}

.history-item:hover {
  background: #f8fafc;
}

.history-item:last-child {
  border-bottom: none;
}

.history-icon {
  width: 48px;
  height: 48px;
  border-radius: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  flex-shrink: 0;
}

.history-icon.recorded {
  background: rgba(16, 185, 129, 0.08);
  color: #10b981;
}

.history-icon.expense {
  background: rgba(245, 158, 11, 0.08);
  color: #f59e0b;
}

.history-content {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.history-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.history-top .source {
  font-weight: 800;
  color: #0f172a;
  font-size: 15px;
}

.history-top .amount {
  font-weight: 800;
  color: #10b981;
  font-size: 16px;
}

.history-top .amount.expense {
  color: #f59e0b;
}

.history-bottom {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.history-bottom .desc {
  font-size: 14px;
  color: #64748b;
  font-weight: 500;
}

.history-bottom .time {
  font-size: 12px;
  color: #94a3b8;
  font-family: monospace;
}

.history-footer {
  padding: 12px;
  display: flex;
  justify-content: center;
  background: #fcfdfe;
}

.empty-text {
  font-size: 13px;
  color: #94a3b8;
}

@media (max-width: 768px) {
  .store-header {
    align-items: flex-start;
    gap: 16px;
  }

  .balance-card {
    padding: 24px;
  }

  .balance-metrics {
    grid-template-columns: 1fr;
  }

  .metric-card .value {
    font-size: 30px;
  }

  .history-top,
  .history-bottom,
  .product-footer {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }
}
</style>
