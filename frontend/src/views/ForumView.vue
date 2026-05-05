<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { 
  Search, 
  Plus, 
  Connection, 
  ChatDotRound, 
  Star, 
  ArrowRight,
  Filter,
  Sort,
  Check,
  Delete
} from '@element-plus/icons-vue'

// --- 类型定义 ---
interface UserBrief {
  user_id: number
  nickname: string
  avatar_url: string
}

interface PlanSquarePost {
  post_id: number
  title: string
  summary: string
  tags: string[]
  author: UserBrief
  template_summary: {
    task_count: number
    mode: string
    start_date: string
    end_date: string
    strategy_labels: string[]
  }
  counters: {
    like_count: number
    comment_count: number
    import_count: number
  }
  viewer_state: {
    liked: boolean
    imported_once: boolean
  }
  status: 'published'
  created_at: string
}

interface CommentNode {
  comment_id: number
  post_id: number
  parent_comment_id: number | null
  content: string
  status: 'visible' | 'deleted'
  author: UserBrief
  can_delete: boolean
  created_at: string
  deleted_at: string | null
  children: CommentNode[]
}

// --- Mock 数据 ---
const mockTags = ['全部', '考研', '高数', '期末', '30天', '英语', '雅思', '自律']
const activeTag = ref('全部')
const searchQuery = ref('')
const sortBy = ref('latest')

const mockPosts = ref<PlanSquarePost[]>([
  {
    post_id: 10001,
    title: "30 天高数强化复习计划",
    summary: "适合期末前一个月快速过完重点题型。本计划涵盖了极限、导数、积分等核心考点，配合历年真题演练，助你高分过关。",
    tags: ["高数", "期末", "30天"],
    author: { user_id: 88, nickname: "小鹿同学", avatar_url: "https://api.dicebear.com/7.x/avataaars/svg?seed=Felix" },
    template_summary: {
      task_count: 24,
      mode: "date_range",
      start_date: "2026-05-05",
      end_date: "2026-06-04",
      strategy_labels: ["每日推进", "错题复盘"]
    },
    counters: { like_count: 128, comment_count: 32, import_count: 45 },
    viewer_state: { liked: false, imported_once: true },
    status: "published",
    created_at: "2026-05-04T20:30:00+08:00"
  },
  {
    post_id: 10002,
    title: "雅思口语 7.5 分冲刺手册",
    summary: "重点攻克 Part 2 和 Part 3。精选 50 个高频话题，包含地道表达和逻辑连接词，适合短期提分。",
    tags: ["英语", "雅思", "口语"],
    author: { user_id: 89, nickname: "杰森英语", avatar_url: "https://api.dicebear.com/7.x/avataaars/svg?seed=Aneka" },
    template_summary: {
      task_count: 15,
      mode: "quantity",
      start_date: "",
      end_date: "",
      strategy_labels: ["录音回听", "范文精读"]
    },
    counters: { like_count: 256, comment_count: 48, import_count: 89 },
    viewer_state: { liked: true, imported_once: false },
    status: "published",
    created_at: "2026-05-03T10:15:00+08:00"
  },
  {
    post_id: 10003,
    title: "程序员减脂健康餐计划",
    summary: "针对久坐人群设计的营养方案。简单易做，控制热量的同时保证脑力输出。包含详细的买菜清单和烹饪步骤。",
    tags: ["自律", "健康", "减脂"],
    author: { user_id: 90, nickname: "代码养生家", avatar_url: "https://api.dicebear.com/7.x/avataaars/svg?seed=James" },
    template_summary: {
      task_count: 21,
      mode: "daily",
      start_date: "",
      end_date: "",
      strategy_labels: ["控糖", "轻断食"]
    },
    counters: { like_count: 64, comment_count: 12, import_count: 28 },
    viewer_state: { liked: false, imported_once: false },
    status: "published",
    created_at: "2026-05-02T15:20:00+08:00"
  }
])

const mockComments = ref<CommentNode[]>([
  {
    comment_id: 50001,
    post_id: 10001,
    parent_comment_id: null,
    content: "这个计划很适合期末冲刺，我已经导入了，感谢分享！",
    status: "visible",
    author: { user_id: 91, nickname: "西瓜同学", avatar_url: "https://api.dicebear.com/7.x/avataaars/svg?seed=Lily" },
    can_delete: true,
    created_at: "2026-05-04T20:40:00+08:00",
    deleted_at: null,
    children: [
      {
        comment_id: 50002,
        post_id: 10001,
        parent_comment_id: 50001,
        content: "同感，特别是错题复盘那个环节设置得很好。",
        status: "visible",
        author: { user_id: 92, nickname: "青柠同学", avatar_url: "https://api.dicebear.com/7.x/avataaars/svg?seed=Jack" },
        can_delete: false,
        created_at: "2026-05-04T20:42:00+08:00",
        deleted_at: null,
        children: []
      }
    ]
  },
  {
    comment_id: 50003,
    post_id: 10001,
    parent_comment_id: null,
    content: "博主能分享一下具体的参考书目吗？",
    status: "visible",
    author: { user_id: 93, nickname: "爱学习的橘子", avatar_url: "https://api.dicebear.com/7.x/avataaars/svg?seed=Bella" },
    can_delete: false,
    created_at: "2026-05-04T21:00:00+08:00",
    deleted_at: null,
    children: []
  }
])

import { useRouter } from 'vue-router'
const router = useRouter()

// --- 状态变量 ---
const selectedPost = ref<PlanSquarePost | null>(null)
const publishDialogVisible = ref(false)
const isSubmitting = ref(false)
const newComment = ref('')

// --- 计算属性 ---
const filteredPosts = computed(() => {
  let result = [...mockPosts.value]
  
  if (activeTag.value !== '全部') {
    result = result.filter(p => p.tags.includes(activeTag.value))
  }
  
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    result = result.filter(p => p.title.toLowerCase().includes(q) || p.summary.toLowerCase().includes(q))
  }
  
  if (sortBy.value === 'likes') {
    result.sort((a, b) => b.counters.like_count - a.counters.like_count)
  } else if (sortBy.value === 'imports') {
    result.sort((a, b) => b.counters.import_count - a.counters.import_count)
  } else {
    result.sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
  }
  
  return result
})

// --- 方法 ---
function openDetails(post: PlanSquarePost) {
  router.push(`/forum/${post.post_id}`)
}

function handleLike(post: PlanSquarePost) {
  if (post.viewer_state.liked) {
    post.viewer_state.liked = false
    post.counters.like_count--
    ElMessage.info('已取消点赞')
  } else {
    post.viewer_state.liked = true
    post.counters.like_count++
    ElMessage.success('点赞成功')
  }
}

async function handleImport(post: PlanSquarePost) {
  try {
    await ElMessageBox.confirm(
      `确定要将《${post.title}》导入到你的计划中吗？`,
      '导入确认',
      { confirmButtonText: '立即导入', cancelButtonText: '取消', type: 'info' }
    )
    
    isSubmitting.value = true
    // 模拟 API 调用延迟
    await new Promise(r => setTimeout(r, 1000))
    
    post.viewer_state.imported_once = true
    post.counters.import_count++
    
    ElMessage.success({
      message: '导入成功！已为你创建新的任务计划。',
      duration: 3000
    })
  } catch {
    // 用户取消
  } finally {
    isSubmitting.value = false
  }
}

function submitComment() {
  if (!newComment.value.trim()) return
  
  const comment: CommentNode = {
    comment_id: Date.now(),
    post_id: selectedPost.value!.post_id,
    parent_comment_id: null,
    content: newComment.value,
    status: 'visible',
    author: { user_id: 1, nickname: "我 (Me)", avatar_url: "https://api.dicebear.com/7.x/avataaars/svg?seed=Lucky" },
    can_delete: true,
    created_at: new Date().toISOString(),
    deleted_at: null,
    children: []
  }
  
  mockComments.value.unshift(comment)
  newComment.value = ''
  ElMessage.success('评论发表成功')
}

function deleteComment(comment: CommentNode) {
  comment.status = 'deleted'
  comment.content = '该评论已删除'
  ElMessage.info('评论已删除')
}

function formatDate(iso: string) {
  const date = new Date(iso)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}
</script>

<template>
  <div class="forum-container">
    <!-- Header -->
    <header class="forum-header">
      <div class="header-left">
        <h1>计划广场</h1>
        <p>发现并分享优质的任务计划模板</p>
      </div>
      
      <div class="header-actions">
        <div class="search-box">
          <el-input
            v-model="searchQuery"
            placeholder="搜索计划、关键词..."
            :prefix-icon="Search"
            clearable
          />
        </div>
        <el-button type="primary" :icon="Plus" round @click="publishDialogVisible = true">
          发布计划
        </el-button>
      </div>
    </header>

    <!-- Filters & Tabs -->
    <div class="forum-filters">
      <div class="tags-scroller">
        <button 
          v-for="tag in mockTags" 
          :key="tag"
          class="tag-chip"
          :class="{ active: activeTag === tag }"
          @click="activeTag = tag"
        >
          {{ tag }}
        </button>
      </div>
      
      <div class="sort-dropdown">
        <el-select v-model="sortBy" placeholder="排序方式" style="width: 120px">
          <el-option label="最新发布" value="latest" />
          <el-option label="最多点赞" value="likes" />
          <el-option label="最多导入" value="imports" />
        </el-select>
      </div>
    </div>

    <!-- Post Grid -->
    <main class="forum-grid">
      <transition-group name="post-list">
        <div 
          v-for="post in filteredPosts" 
          :key="post.post_id" 
          class="post-card"
          @click="openDetails(post)"
        >
          <div class="post-card__header">
            <h3 class="post-title">{{ post.title }}</h3>
            <div class="post-tags">
              <span v-for="tag in post.tags.slice(0, 3)" :key="tag" class="small-tag">{{ tag }}</span>
            </div>
          </div>
          
          <p class="post-summary">{{ post.summary }}</p>
          
          <div class="post-card__footer">
            <div class="author-info">
              <img :src="post.author.avatar_url" class="author-avatar" />
              <span class="author-name">{{ post.author.nickname }}</span>
            </div>
            
            <div class="post-stats">
              <span class="stat-item" :class="{ active: post.viewer_state.liked }" @click.stop="handleLike(post)">
                <el-icon><Star /></el-icon> {{ post.counters.like_count }}
              </span>
              <span class="stat-item">
                <el-icon><ChatDotRound /></el-icon> {{ post.counters.comment_count }}
              </span>
              <span class="stat-item" :class="{ imported: post.viewer_state.imported_once }" @click.stop="handleImport(post)">
                <el-icon><Connection /></el-icon> {{ post.counters.import_count }}
              </span>
            </div>
          </div>
        </div>
      </transition-group>
      
      <!-- Empty State -->
      <div v-if="filteredPosts.length === 0" class="empty-state">
        <el-empty description="暂无符合条件的计划" />
      </div>
    </main>

    <!-- Publish Dialog -->
    <el-dialog
      v-model="publishDialogVisible"
      title="发布新计划"
      width="500px"
      append-to-body
    >
      <el-form label-position="top">
        <el-form-item label="选择现有计划模板" required>
          <el-select placeholder="请选择你的 TaskClass" style="width: 100%">
            <el-option label="我的 2026 高数笔记" value="1" />
            <el-option label="每日算法 100 题" value="2" />
          </el-select>
        </el-form-item>
        <el-form-item label="标题" required>
          <el-input placeholder="给你的计划起个吸引人的名字 (4-40字)" />
        </el-form-item>
        <el-form-item label="简介">
          <el-input type="textarea" :rows="3" placeholder="详细描述一下这个计划的适用人群和优势..." />
        </el-form-item>
        <el-form-item label="标签">
          <el-select multiple filterable allow-create default-first-option placeholder="添加标签 (最多5个)">
            <el-option v-for="t in mockTags.slice(1)" :key="t" :label="t" :value="t" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="publishDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="publishDialogVisible = false; ElMessage.success('发布成功！审核通过后将展示在广场。')">
          确认发布
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.forum-container {
  height: 100%;
  display: flex;
  flex-direction: column;
  background: #f8fafc;
  overflow-y: auto;
}

/* Header */
.forum-header {
  background: #fff;
  padding: 24px 40px;
  border-bottom: 1px solid #f1f5f9;
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

.header-actions {
  display: flex;
  gap: 16px;
  align-items: center;
}

.search-box {
  width: 280px;
}

/* Filters */
.forum-filters {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 20px;
  padding: 0 40px;
}

.tags-scroller {
  display: flex;
  gap: 8px;
  overflow-x: auto;
  padding-bottom: 4px;
}

.tags-scroller::-webkit-scrollbar {
  height: 4px;
}

.tag-chip {
  padding: 6px 16px;
  border-radius: 20px;
  border: 1px solid #e2e8f0;
  background: #fff;
  color: #64748b;
  font-size: 13px;
  cursor: pointer;
  transition: all 0.2s;
  white-space: nowrap;
}

.tag-chip:hover {
  border-color: #3b82f6;
  color: #3b82f6;
}

.tag-chip.active {
  background: #3b82f6;
  border-color: #3b82f6;
  color: #fff;
  box-shadow: 0 4px 10px rgba(59, 130, 246, 0.2);
}

/* Dialog Styling */
:deep(.el-dialog) {
  border-radius: 24px !important;
  padding: 32px !important;
  box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.15) !important;
}

:deep(.el-dialog__header) {
  padding: 0 0 24px 0 !important;
  margin: 0 !important;
}

:deep(.el-dialog__title) {
  font-size: 24px !important;
  font-weight: 800 !important;
  color: #0f172a !important;
}

:deep(.el-dialog__body) {
  padding: 0 !important;
}

.publish-form {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 32px;
}

.publish-btn {
  padding: 12px 32px !important;
  font-weight: 700 !important;
  letter-spacing: 0.5px;
}

/* Post Grid */
.forum-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 24px;
  padding: 0 40px 40px;
}

.post-card {
  background: #fff;
  border-radius: 20px;
  padding: 24px;
  border: 1px solid #f1f5f9;
  cursor: pointer;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  display: flex;
  flex-direction: column;
  gap: 16px;
  position: relative;
  overflow: hidden;
}

.post-card:hover {
  transform: translateY(-4px);
  box-shadow: 0 12px 30px rgba(0, 0, 0, 0.04);
  border-color: #dbeafe;
}

.post-title {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  color: #1e293b;
  line-height: 1.4;
}

.post-tags {
  display: flex;
  gap: 6px;
  margin-top: 6px;
}

.small-tag {
  font-size: 11px;
  color: #3b82f6;
  background: rgba(59, 130, 246, 0.08);
  padding: 2px 8px;
  border-radius: 4px;
}

/* --- Global Form Overrides (Flat & Clean) --- */
:deep(.el-input__inner) {
  border-radius: 12px !important;
  background-color: #f1f5f9 !important;
  border: 1px solid transparent !important;
  transition: all 0.2s ease !important;
  color: #1e293b !important;
}

:deep(.el-input__inner:focus) {
  background-color: #fff !important;
  border-color: #3b82f6 !important;
  box-shadow: 0 0 0 4px rgba(59, 130, 246, 0.1) !important;
}

:deep(.el-textarea__inner) {
  border-radius: 12px !important;
  background-color: #f1f5f9 !important;
  border: 1px solid transparent !important;
  padding: 12px 16px !important;
  transition: all 0.2s ease !important;
}

:deep(.el-textarea__inner:focus) {
  background-color: #fff !important;
  border-color: #3b82f6 !important;
  box-shadow: 0 0 0 4px rgba(59, 130, 246, 0.1) !important;
}

:deep(.el-select .el-input__inner) {
  background-color: #fff !important;
  border: 1px solid #e2e8f0 !important;
  border-radius: 10px !important;
}

/* Removed duplicate .forum-container and merged .forum-header */

.header-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.search-wrapper {
  width: 400px;
}

.search-input :deep(.el-input__wrapper) {
  box-shadow: none !important;
  background: #f1f5f9 !important;
  border-radius: 14px !important;
  padding: 4px 16px !important;
}

.header-bottom {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.category-bar {
  display: flex;
  gap: 12px;
}

.category-tag {
  padding: 8px 18px;
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 12px;
  font-size: 14px;
  font-weight: 600;
  color: #64748b;
  cursor: pointer;
  transition: all 0.2s ease;
}

.category-tag:hover {
  border-color: #3b82f6;
  color: #3b82f6;
}

.category-tag.active {
  background: #3b82f6;
  border-color: #3b82f6;
  color: #fff;
  box-shadow: 0 4px 12px rgba(59, 130, 246, 0.2);
}

.filter-bar {
  display: flex;
  align-items: center;
  gap: 12px;
}

.sort-select {
  width: 140px;
}

.post-summary {
  color: #64748b;
  font-size: 14px;
  line-height: 1.6;
  margin: 0;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.post-card__footer {
  margin-top: auto;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 12px;
  border-top: 1px solid #f8fafc;
}

.author-info {
  display: flex;
  align-items: center;
  gap: 8px;
}

.author-avatar {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: #f1f5f9;
}

.author-name {
  font-size: 13px;
  color: #475569;
  font-weight: 500;
}

.post-stats {
  display: flex;
  gap: 12px;
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: #94a3b8;
  transition: all 0.2s;
}

.stat-item.active {
  color: #f43f5e;
}

.stat-item.active .el-icon {
  fill: #f43f5e;
}

.stat-item.imported {
  color: #10b981;
}

/* Drawer Styles */
.drawer-content {
  padding: 0 4px;
  display: flex;
  flex-direction: column;
  gap: 32px;
}

.author-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 20px;
}

.large-avatar {
  width: 44px;
  height: 44px;
  border-radius: 50%;
  background: #f1f5f9;
}

.author-meta {
  flex: 1;
}

.author-meta .name {
  font-weight: 700;
  color: #1e293b;
}

.author-meta .time {
  font-size: 12px;
  color: #94a3b8;
}

.detail-title {
  font-size: 24px;
  font-weight: 800;
  margin: 0 0 12px 0;
  line-height: 1.3;
}

.detail-tags {
  margin-bottom: 16px;
}

.detail-summary {
  color: #475569;
  line-height: 1.7;
  font-size: 15px;
}

.detail-section h4 {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 700;
  margin: 0 0 16px 0;
  color: #1e293b;
}

.template-preview {
  background: #f8fafc;
  border-radius: 12px;
  padding: 16px;
  border: 1px solid #f1f5f9;
}

.preview-meta {
  display: flex;
  gap: 20px;
  font-size: 13px;
  color: #64748b;
  margin-bottom: 12px;
}

.strategy-labels {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}

.strategy-badge {
  font-size: 11px;
  background: #fff;
  border: 1px solid #e2e8f0;
  padding: 2px 8px;
  border-radius: 4px;
  color: #475569;
}

.preview-items {
  list-style: none;
  padding: 0;
  margin: 0;
  font-size: 13px;
  color: #475569;
}

.preview-items li {
  padding: 4px 0;
}

.preview-items li.more {
  color: #94a3b8;
  font-style: italic;
  margin-top: 4px;
}

/* Comments Section */
.comment-input-box {
  margin-bottom: 24px;
}

.input-actions {
  display: flex;
  justify-content: flex-end;
  margin-top: 8px;
}

.comments-list {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.comment-item {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.comment-main {
  display: flex;
  gap: 12px;
}

.comment-avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: #f1f5f9;
}

.comment-avatar.small {
  width: 24px;
  height: 24px;
}

.comment-body {
  flex: 1;
}

.comment-user {
  font-size: 13px;
  font-weight: 700;
  color: #475569;
  display: flex;
  align-items: center;
  gap: 8px;
}

.comment-time {
  font-weight: 400;
  font-size: 11px;
  color: #94a3b8;
}

.comment-content {
  font-size: 14px;
  color: #1e293b;
  margin: 4px 0;
  line-height: 1.5;
}

.comment-content.deleted {
  color: #cbd5e1;
  font-style: italic;
}

.comment-actions {
  display: flex;
  gap: 12px;
}

.action-btn {
  font-size: 12px;
  color: #3b82f6;
  cursor: pointer;
  opacity: 0.8;
}

.action-btn:hover {
  opacity: 1;
}

.action-btn.delete {
  color: #ef4444;
}

.comment-children {
  margin-left: 44px;
  padding-left: 12px;
  border-left: 2px solid #f1f5f9;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* Animations */
.post-list-enter-active,
.post-list-leave-active {
  transition: all 0.5s ease;
}
.post-list-enter-from,
.post-list-leave-to {
  opacity: 0;
  transform: translateY(30px);
}
</style>
