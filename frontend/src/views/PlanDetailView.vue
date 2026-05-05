<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { 
  ArrowLeft,
  Connection, 
  ChatDotRound, 
  Star, 
  Filter,
  Check
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

const route = useRoute()
const router = useRouter()
const isLoading = ref(true)
const selectedPost = ref<PlanSquarePost | null>(null)
const mockComments = ref<CommentNode[]>([])
const newComment = ref('')
const replyingToId = ref<number | null>(null)
const replyText = ref('')

// --- 初始化 Mock 数据 ---
onMounted(async () => {
  // 模拟加载延迟
  await new Promise(r => setTimeout(r, 600))
  
  const postId = Number(route.params.id)
  
  // 模拟从后端获取详情
  selectedPost.value = {
    post_id: postId,
    title: postId === 10001 ? "30 天高数强化复习计划 (深度进阶版)" : "雅思口语 7.5 分冲刺手册",
    summary: `这是一份经过验证的高质量计划，帮助你快速达成目标。
    
    本计划不仅涵盖了基础知识点，还深入探讨了高数中最为棘手的证明题与综合题型。
    在接下来的30天里，我们将通过系统的拆解，将复杂的微积分问题简化为可执行的每日任务。
    无论你是为了考研冲刺，还是期末突击，这份计划都将是你最坚实的后盾。
    
    我们将重点关注以下几个模块：
    1. 函数、极限与连续的深度理解
    2. 一元函数微分学的应用技巧
    3. 积分学的各种变换与计算模型
    4. 空间解析几何与向量代数
    5. 多元函数微分与积分学
    6. 常微分方程的特殊解法
    
    每个模块都配备了精选的例题和课后练习，确保你能学以致用。
    请务必严格按照计划执行，不要遗漏任何一个复盘环节。
    祝你在数学的海洋中乘风破浪，取得优异成绩！`,
    tags: ["高数", "复习", "冲刺", "考研", "干货"],
    author: { user_id: 88, nickname: "小鹿同学", avatar_url: "https://api.dicebear.com/7.x/avataaars/svg?seed=Felix" },
    template_summary: {
      task_count: 30,
      mode: "date_range",
      start_date: "2026-05-05",
      end_date: "2026-06-04",
      strategy_labels: ["每日推进", "错题复盘", "阶段测试", "脑图总结"]
    },
    counters: { like_count: 128, comment_count: 32, import_count: 45 },
    viewer_state: { liked: false, imported_once: false },
    status: "published",
    created_at: "2026-05-04T20:30:00+08:00"
  }

  mockComments.value = Array.from({ length: 15 }).map((_, i) => ({
    comment_id: 50000 + i,
    post_id: postId,
    parent_comment_id: null,
    content: `这是第 ${i + 1} 条测试评论。这份计划真的太详细了，特别是关于${['微积分', '中值定理', '泰勒公式', '多重积分'][i % 4]}的部分，讲得非常透彻。`,
    status: "visible",
    author: { 
      user_id: 100 + i, 
      nickname: `学霸${i + 1}号`, 
      avatar_url: `https://api.dicebear.com/7.x/avataaars/svg?seed=User${i}` 
    },
    can_delete: true, // 全部允许删除以便测试
    created_at: "2026-05-04T20:40:00+08:00",
    deleted_at: null,
    children: i === 0 ? [
      {
        comment_id: 60001,
        post_id: postId,
        parent_comment_id: 50000,
        content: "我也这么觉得，博主太用心了！",
        status: "visible",
        author: { user_id: 201, nickname: "回复人A", avatar_url: "https://api.dicebear.com/7.x/avataaars/svg?seed=A" },
        can_delete: true,
        created_at: "2026-05-04T21:00:00+08:00",
        deleted_at: null,
        children: []
      }
    ] : []
  }))
  
  isLoading.value = false
})

function goBack() {
  router.push('/forum')
}

function handleLike() {
  if (!selectedPost.value) return
  if (selectedPost.value.viewer_state.liked) {
    selectedPost.value.viewer_state.liked = false
    selectedPost.value.counters.like_count--
  } else {
    selectedPost.value.viewer_state.liked = true
    selectedPost.value.counters.like_count++
    ElMessage.success('点赞成功')
  }
}

async function handleImport() {
  if (!selectedPost.value) return
  isLoading.value = true
  await new Promise(r => setTimeout(r, 800))
  selectedPost.value.viewer_state.imported_once = true
  selectedPost.value.counters.import_count++
  ElMessage.success('导入成功')
  isLoading.value = false
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
  ElMessage.success('发表成功')
}

function startReply(commentId: number) {
  replyingToId.value = commentId
  replyText.value = ''
}

function submitReply(parentComment: CommentNode) {
  if (!replyText.value.trim()) return
  const reply: CommentNode = {
    comment_id: Date.now(),
    post_id: selectedPost.value!.post_id,
    parent_comment_id: parentComment.comment_id,
    content: replyText.value,
    status: 'visible',
    author: { user_id: 1, nickname: "我 (Me)", avatar_url: "https://api.dicebear.com/7.x/avataaars/svg?seed=Lucky" },
    can_delete: true,
    created_at: new Date().toISOString(),
    deleted_at: null,
    children: []
  }
  parentComment.children.push(reply)
  replyingToId.value = null
  replyText.value = ''
  ElMessage.success('回复成功')
}

function deleteComment(commentId: number) {
  // 递归删除逻辑
  const removeRecursive = (list: CommentNode[], id: number): boolean => {
    for (let i = 0; i < list.length; i++) {
      if (list[i].comment_id === id) {
        list.splice(i, 1)
        return true
      }
      if (list[i].children && removeRecursive(list[i].children, id)) {
        return true
      }
    }
    return false
  }
  
  if (removeRecursive(mockComments.value, commentId)) {
    ElMessage.success('评论已删除')
  }
}

function formatDate(iso: string) {
  const date = new Date(iso)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}
</script>

<template>
  <div class="detail-page-container" v-loading="isLoading">
    <div v-if="selectedPost" class="detail-wrapper">
      <!-- Top Navigation -->
      <nav class="detail-nav">
        <el-button :icon="ArrowLeft" circle @click="goBack" />
        <span class="nav-title">计划详情</span>
        <div class="nav-actions">
          <el-button 
            type="primary" 
            round 
            :icon="selectedPost.viewer_state.imported_once ? Check : Connection"
            @click="handleImport"
          >
            {{ selectedPost.viewer_state.imported_once ? '已导入' : '立即导入' }}
          </el-button>
        </div>
      </nav>

      <!-- Main Content -->
      <div class="detail-main-layout">
        <aside class="detail-sidebar">
          <div class="author-card">
            <img :src="selectedPost.author.avatar_url" class="author-avatar" />
            <div class="author-name">{{ selectedPost.author.nickname }}</div>
            <div class="publish-time">发布于 {{ formatDate(selectedPost.created_at) }}</div>
            <div class="author-stats">
              <div class="stat-box">
                <span class="num">{{ selectedPost.counters.like_count }}</span>
                <span class="lbl">获赞</span>
              </div>
              <div class="stat-box">
                <span class="num">{{ selectedPost.counters.import_count }}</span>
                <span class="lbl">导入</span>
              </div>
            </div>
          </div>

          <div class="template-summary-card">
            <h4><el-icon><Filter /></el-icon> 计划概览</h4>
            <div class="summary-info">
              <div class="info-row">
                <span class="label">任务总数</span>
                <span class="value">{{ selectedPost.template_summary.task_count }}</span>
              </div>
              <div class="info-row">
                <span class="label">排程模式</span>
                <span class="value">{{ selectedPost.template_summary.mode === 'date_range' ? '日期范围' : '固定天数' }}</span>
              </div>
            </div>
            <div class="strategy-list">
              <span v-for="tag in selectedPost.template_summary.strategy_labels" :key="tag" class="strategy-tag">
                {{ tag }}
              </span>
            </div>
          </div>

          <div class="tasks-overview-card">
            <h4><el-icon><Filter /></el-icon> 任务预览</h4>
            <div class="items-list">
              <div v-for="i in 10" :key="i" class="item-node">
                <div class="node-idx">{{ String(i).padStart(2, '0') }}</div>
                <div class="node-content">
                  {{ [
                    '复习极限与连续的基础概念，完成课后习题。',
                    '导数与微分的中值定理深度解析，配合真题演练。',
                    '泰勒展开式及其在近似计算中的应用技巧。',
                    '不定积分的换元法与分部积分法专项突破。',
                    '定积分的几何意义与物理应用案例分析。',
                    '多元函数偏导数与全微分的计算模型。',
                    '二重积分在极坐标下的变换与计算方法。',
                    '常微分方程的一阶线性方程求解步骤。',
                    '向量代数与空间解析几何的综合练习。',
                    '全书重点难点回顾与思维导图梳理。'
                  ][(i-1) % 10] }}
                </div>
              </div>
              <div class="item-node more">
                ... 更多 {{ selectedPost.template_summary.task_count - 10 }} 个任务项 ...
              </div>
            </div>
          </div>
        </aside>

        <main class="detail-content-area">
          <section class="content-header">
            <h1 class="plan-title">{{ selectedPost.title }}</h1>
            <div class="plan-tags">
              <el-tag v-for="tag in selectedPost.tags" :key="tag" class="mx-1" effect="plain">{{ tag }}</el-tag>
            </div>
            <p class="plan-description">{{ selectedPost.summary }}</p>
          </section>

          <section class="comments-section">
            <h3><el-icon><ChatDotRound /></el-icon> 互动区 ({{ selectedPost.counters.comment_count }})</h3>
            
            <div class="comment-post-box">
              <img src="https://api.dicebear.com/7.x/avataaars/svg?seed=Lucky" class="current-user-avatar" />
              <div class="input-container">
                <el-input
                  v-model="newComment"
                  type="textarea"
                  :rows="2"
                  placeholder="写下你的想法，与大家交流..."
                  resize="none"
                  class="premium-input"
                />
                <div class="post-actions">
                  <el-button type="primary" round size="default" @click="submitComment">发布评论</el-button>
                </div>
              </div>
            </div>

            <div class="comments-list">
              <div v-for="comment in mockComments" :key="comment.comment_id" class="comment-item">
                <img :src="comment.author.avatar_url" class="c-avatar" />
                <div class="c-body">
                  <div class="c-user">
                    {{ comment.author.nickname }}
                    <span class="c-time">{{ formatDate(comment.created_at) }}</span>
                  </div>
                  <div class="c-content">{{ comment.content }}</div>
                  <div class="c-actions">
                    <span class="btn" @click="startReply(comment.comment_id)">回复</span>
                    <span v-if="comment.can_delete" class="btn del" @click="deleteComment(comment.comment_id)">删除</span>
                  </div>

                  <!-- Reply Input -->
                  <transition name="el-zoom-in-top">
                    <div v-if="replyingToId === comment.comment_id" class="reply-input-wrapper">
                      <el-input 
                        v-model="replyText" 
                        type="textarea"
                        :rows="1"
                        auto-grow
                        placeholder="写下你的回复..." 
                        class="reply-field"
                        @keyup.enter.ctrl="submitReply(comment)"
                      />
                      <div class="reply-btns">
                        <el-button size="small" link @click="replyingToId = null">取消</el-button>
                        <el-button size="small" type="primary" round @click="submitReply(comment)">提交回复</el-button>
                      </div>
                    </div>
                  </transition>

                  <!-- Child Comments -->
                  <div v-if="comment.children.length > 0" class="comment-children">
                    <div v-for="child in comment.children" :key="child.comment_id" class="comment-item child">
                      <img :src="child.author.avatar_url" class="c-avatar small" />
                      <div class="c-body">
                        <div class="c-user">
                          {{ child.author.nickname }}
                          <span class="c-time">{{ formatDate(child.created_at) }}</span>
                        </div>
                        <div class="c-content">{{ child.content }}</div>
                        <div class="c-actions">
                          <span class="btn" @click="startReply(child.comment_id)">回复</span>
                          <span v-if="child.can_delete" class="btn del" @click="deleteComment(child.comment_id)">删除</span>
                        </div>

                        <!-- Reply Input for child -->
                        <transition name="el-zoom-in-top">
                          <div v-if="replyingToId === child.comment_id" class="reply-input-wrapper">
                            <el-input 
                              v-model="replyText" 
                              type="textarea"
                              :rows="1"
                              placeholder="写下你的回复..." 
                              class="reply-field"
                              @keyup.enter.ctrl="submitReply(child)"
                            />
                            <div class="reply-btns">
                              <el-button size="small" link @click="replyingToId = null">取消</el-button>
                              <el-button size="small" type="primary" round @click="submitReply(child)">提交回复</el-button>
                            </div>
                          </div>
                        </transition>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </main>
      </div>
    </div>
  </div>
</template>

<style scoped>
.detail-page-container {
  height: 100%;
  background: #f8fafc;
  display: flex;
  flex-direction: column;
  overflow: hidden; /* 彻底禁止整页滚动 */
}

.detail-wrapper {
  width: 100%;
  height: 100%;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  box-sizing: border-box;
}

.detail-nav {
  display: flex;
  align-items: center;
  gap: 16px;
  background: #fff; /* 独立滚动后不再需要毛玻璃，纯白更稳重 */
  padding: 16px 24px;
  border-radius: 20px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.03);
  flex-shrink: 0;
  z-index: 1000;
}

.nav-title {
  font-weight: 700;
  font-size: 18px;
  color: #1e293b;
  flex: 1;
}

.detail-main-layout {
  display: grid;
  grid-template-columns: 320px 1fr;
  gap: 24px;
  align-items: stretch; /* 占满高度 */
  flex: 1;
  min-height: 0; /* 关键：允许 flex 子项缩小 */
  overflow: hidden;
}

/* Sidebar */
.detail-sidebar {
  display: flex;
  flex-direction: column;
  gap: 24px;
  height: 100%;
  overflow-y: auto;
  padding-right: 4px; /* 为侧边栏滚动留出空间 */
}

/* 隐藏侧边栏总滚动条，仅内部卡片滚动或整体滚动 */
.detail-sidebar::-webkit-scrollbar {
  width: 0;
}

.author-card {
  background: #fff;
  border-radius: 24px;
  padding: 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.05);
  border: 1px solid #f1f5f9;
}

.author-avatar {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  background: #f1f5f9;
  margin-bottom: 12px;
  border: 3px solid #f8fafc;
}

.author-name {
  font-weight: 800;
  font-size: 16px;
  color: #0f172a;
}

.publish-time {
  font-size: 11px;
  color: #94a3b8;
  margin-top: 4px;
}

.author-stats {
  display: flex;
  width: 100%;
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid #f1f5f9;
}

.stat-box {
  flex: 1;
  display: flex;
  flex-direction: column;
}

.stat-box .num {
  font-weight: 800;
  font-size: 16px;
  color: #1e293b;
}

.stat-box .lbl {
  font-size: 11px;
  color: #64748b;
}

.template-summary-card, .tasks-overview-card {
  background: #fff;
  border-radius: 24px;
  padding: 20px;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.05);
  border: 1px solid #f1f5f9;
}

.tasks-overview-card {
  display: flex;
  flex-direction: column;
  max-height: 400px;
  overflow: hidden;
}

.tasks-overview-card h4 {
  margin: 0 0 16px 0;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.tasks-overview-card .items-list {
  overflow-y: auto;
  padding-right: 4px;
}

.tasks-overview-card .items-list::-webkit-scrollbar {
  width: 4px;
}

.tasks-overview-card .items-list::-webkit-scrollbar-thumb {
  background: #e2e8f0;
  border-radius: 10px;
}

.template-summary-card h4 {
  margin: 0 0 16px 0;
  display: flex;
  align-items: center;
  gap: 8px;
}

.summary-info {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.info-row {
  display: flex;
  justify-content: space-between;
  font-size: 14px;
}

.info-row .label { color: #64748b; }
.info-row .value { font-weight: 600; color: #1e293b; }

.strategy-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 16px;
}

.strategy-tag {
  font-size: 11px;
  background: #f1f5f9;
  color: #475569;
  padding: 4px 10px;
  border-radius: 6px;
}

/* Content Area */
.detail-content-area {
  background: #fff;
  border-radius: 24px;
  padding: 40px 60px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.03);
  display: flex;
  flex-direction: column;
  gap: 40px;
  height: 100%;
  overflow-y: auto;
}

.detail-content-area::-webkit-scrollbar {
  width: 6px;
}

.detail-content-area::-webkit-scrollbar-thumb {
  background: #e2e8f0;
  border-radius: 10px;
}

.detail-content-area::-webkit-scrollbar-track {
  background: transparent;
}

.plan-title {
  font-size: 32px;
  font-weight: 800;
  margin: 0 0 16px 0;
  color: #0f172a;
  line-height: 1.3;
}

.plan-tags { margin-bottom: 24px; }

.plan-description {
  font-size: 18px;
  line-height: 1.8;
  color: #475569;
  white-space: pre-wrap;
}

.content-body h3, .comments-section h3 {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 20px;
  font-weight: 700;
  margin: 0 0 24px 0;
}

.items-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.item-node {
  display: flex;
  gap: 12px;
  background: #f8fafc;
  padding: 12px;
  border-radius: 12px;
  border: 1px solid #f1f5f9;
  margin-bottom: 8px;
}

.node-idx {
  font-weight: 800;
  color: #3b82f6;
  font-family: monospace;
  font-size: 14px;
}

.node-content {
  color: #1e293b;
  font-size: 13px;
  line-height: 1.4;
}

.item-node.more {
  justify-content: center;
  background: transparent;
  border: 1px dashed #e2e8f0;
  color: #94a3b8;
  font-size: 12px;
  font-style: italic;
  padding: 8px;
}

/* Comments */
.comment-post-box {
  background: #fff;
  padding: 24px;
  border-radius: 20px;
  margin-bottom: 40px;
  display: flex;
  gap: 16px;
  border: 1px solid #f1f5f9;
  box-shadow: 0 4px 15px rgba(0, 0, 0, 0.02);
  transition: all 0.3s ease;
}

.comment-post-box:focus-within {
  box-shadow: 0 8px 25px rgba(59, 130, 246, 0.08);
  border-color: #dbeafe;
}

.current-user-avatar {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  flex-shrink: 0;
}

.input-container {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.premium-input :deep(.el-textarea__inner) {
  border: none;
  background: #f8fafc;
  padding: 12px 16px;
  border-radius: 12px;
  font-size: 15px;
  color: #1e293b;
  transition: all 0.2s ease;
}

.premium-input :deep(.el-textarea__inner:focus) {
  background: #fff;
  box-shadow: inset 0 0 0 1px #3b82f6;
}

.post-actions {
  display: flex;
  justify-content: flex-end;
}

.comments-list {
  display: flex;
  flex-direction: column;
  gap: 32px;
}

.comment-item {
  display: flex;
  gap: 16px;
}

.c-avatar {
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: #f1f5f9;
}

.c-body { flex: 1; }

.c-user {
  font-weight: 700;
  font-size: 15px;
  color: #0f172a;
  margin-bottom: 6px;
}

.c-time {
  font-weight: 400;
  font-size: 12px;
  color: #94a3b8;
  margin-left: 10px;
}

.c-content {
  font-size: 15px;
  color: #334155;
  line-height: 1.6;
  white-space: pre-wrap;
}

.c-actions {
  margin-top: 10px;
  display: flex;
  gap: 16px;
  margin-bottom: 12px;
}

.btn {
  font-size: 13px;
  font-weight: 600;
  color: #64748b;
  cursor: pointer;
  user-select: none;
  transition: color 0.2s;
}

.btn:hover { color: #3b82f6; }
.btn.del:hover { color: #ef4444; }

.reply-input-wrapper {
  margin: 12px 0 20px 0;
  background: #f8fafc;
  padding: 16px;
  border-radius: 16px;
  border: 1px solid #f1f5f9;
  box-shadow: 0 4px 10px rgba(0, 0, 0, 0.02);
}

.reply-field :deep(.el-textarea__inner) {
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  padding: 8px 12px;
  background: #fff;
  font-size: 14px;
}

.reply-btns {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 12px;
}

.comment-children {
  margin-top: 16px;
  padding-left: 20px;
  border-left: 2px solid #f1f5f9;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.c-avatar.small {
  width: 32px;
  height: 32px;
  border-radius: 8px;
}
</style>
