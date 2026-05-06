<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, ChatDotRound, Check, Connection, Filter } from '@element-plus/icons-vue'

import {
  createForumComment,
  deleteForumComment,
  getForumPostDetail,
  importForumPost,
  listForumComments,
} from '@/api/forum'
import type { ForumCommentNode, ForumPostBrief, ForumPostDetail, ForumTemplateDetail } from '@/types/forum'

interface ForumPostDetailView extends ForumPostBrief {
  template: ForumTemplateDetail
}

const route = useRoute()
const router = useRouter()

const isLoading = ref(true)
const commentSubmitting = ref(false)
const selectedPost = ref<ForumPostDetailView | null>(null)
const comments = ref<ForumCommentNode[]>([])
const newComment = ref('')
const replyingToId = ref<number | null>(null)
const replyText = ref('')

const previewItems = computed(() => selectedPost.value?.template.items_preview ?? [])
const previewOverflowCount = computed(() => {
  if (!selectedPost.value) {
    return 0
  }
  return Math.max(selectedPost.value.template.task_count - previewItems.value.length, 0)
})

function goBack() {
  router.push('/forum')
}

function formatDate(iso: string) {
  const date = new Date(iso)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

function formatTemplateMode(mode: string) {
  switch ((mode || '').trim()) {
    case 'date_range':
      return '日期范围'
    case 'quantity':
      return '固定数量'
    case 'daily':
      return '每日模式'
    default:
      return mode || '未设置'
  }
}

function resolvePostId() {
  const value = Number(route.params.id)
  return Number.isInteger(value) && value > 0 ? value : 0
}

function hydratePostDetail(detail: ForumPostDetail) {
  selectedPost.value = {
    ...detail.post,
    template: detail.template,
  }
}

function startReply(commentId: number) {
  replyingToId.value = commentId
  replyText.value = ''
}

function resetReplyState() {
  replyingToId.value = null
  replyText.value = ''
}

function increaseCommentCount(delta: number) {
  if (!selectedPost.value) {
    return
  }
  selectedPost.value.counters.comment_count = Math.max(0, selectedPost.value.counters.comment_count + delta)
}

// patchCommentTree 负责在本地评论树里原地更新指定节点。
// 职责边界：
// 1. 只负责递归定位 comment_id 并执行更新函数，不负责网络请求；
// 2. 找到目标后立即返回 true，避免重复修改多处节点；
// 3. 未找到时返回 false，调用方自行决定是否需要兜底刷新。
function patchCommentTree(list: ForumCommentNode[], commentID: number, updater: (node: ForumCommentNode) => void): boolean {
  for (const node of list) {
    if (node.comment_id === commentID) {
      updater(node)
      return true
    }
    if (patchCommentTree(node.children, commentID, updater)) {
      return true
    }
  }
  return false
}

// appendReplyToTree 负责把新回复挂到命中的父评论 children 下。
// 职责边界：
// 1. 只处理内存树插入，不修改计数和输入框状态；
// 2. 命中父节点后统一插到 children 头部，保证新回复优先可见；
// 3. 若整棵树都没找到则返回 false，由上层决定是否整页重拉。
function appendReplyToTree(list: ForumCommentNode[], parentCommentID: number, reply: ForumCommentNode): boolean {
  for (const node of list) {
    if (node.comment_id === parentCommentID) {
      node.children = [reply, ...node.children]
      return true
    }
    if (appendReplyToTree(node.children, parentCommentID, reply)) {
      return true
    }
  }
  return false
}

async function loadPostPage() {
  const postID = resolvePostId()
  if (postID <= 0) {
    ElMessage.error('计划详情参数无效')
    goBack()
    return
  }

  isLoading.value = true

  try {
    const [detail, commentPage] = await Promise.all([
      getForumPostDetail(postID),
      listForumComments(postID, {
        page: 1,
        page_size: 100,
        sort: 'latest',
      }),
    ])

    hydratePostDetail(detail)
    comments.value = commentPage.items ?? []
    resetReplyState()
    newComment.value = ''
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '计划详情加载失败，请稍后重试')
    goBack()
  } finally {
    isLoading.value = false
  }
}

async function handleImport() {
  if (!selectedPost.value) {
    return
  }
  if (selectedPost.value.viewer_state.imported_once) {
    ElMessage.info('这个计划已经导入过啦')
    return
  }

  try {
    const result = await importForumPost(selectedPost.value.post_id, selectedPost.value.title)
    selectedPost.value.viewer_state.imported_once = true
    selectedPost.value.counters.import_count = result.import_count
    ElMessage.success(`导入成功，已创建任务类《${result.task_class_title}》`)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '导入计划失败，请稍后重试')
  }
}

async function submitComment() {
  const content = newComment.value.trim()
  if (!selectedPost.value || !content || commentSubmitting.value) {
    return
  }

  commentSubmitting.value = true

  try {
    const comment = await createForumComment(selectedPost.value.post_id, { content })
    comments.value = [comment, ...comments.value]
    newComment.value = ''
    increaseCommentCount(1)
    ElMessage.success('评论发布成功')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '发表评论失败，请稍后重试')
  } finally {
    commentSubmitting.value = false
  }
}

async function submitReply(parentComment: ForumCommentNode) {
  const content = replyText.value.trim()
  if (!selectedPost.value || !content || commentSubmitting.value) {
    return
  }

  commentSubmitting.value = true

  try {
    const reply = await createForumComment(selectedPost.value.post_id, {
      content,
      parent_comment_id: parentComment.comment_id,
    })

    if (!appendReplyToTree(comments.value, parentComment.comment_id, reply)) {
      comments.value = [reply, ...comments.value]
    }

    increaseCommentCount(1)
    resetReplyState()
    ElMessage.success('回复发布成功')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '回复失败，请稍后重试')
  } finally {
    commentSubmitting.value = false
  }
}

async function handleDeleteComment(commentID: number) {
  if (commentSubmitting.value) {
    return
  }

  commentSubmitting.value = true

  try {
    const result = await deleteForumComment(commentID)
    if (!patchCommentTree(comments.value, commentID, (node) => {
      node.status = result.status
      node.content = result.content
      node.deleted_at = result.deleted_at
      node.can_delete = false
    })) {
      await loadPostPage()
      return
    }

    increaseCommentCount(-1)
    ElMessage.success('评论已删除')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '删除评论失败，请稍后重试')
  } finally {
    commentSubmitting.value = false
  }
}

watch(
  () => route.params.id,
  () => {
    void loadPostPage()
  },
  { immediate: true },
)
</script>
<template>
  <div class="detail-page-container" v-loading="isLoading">
    <div v-if="selectedPost" class="detail-wrapper">
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
                <span class="value">{{ selectedPost.template.task_count }}</span>
              </div>
              <div class="info-row">
                <span class="label">排程模式</span>
                <span class="value">{{ formatTemplateMode(selectedPost.template.mode) }}</span>
              </div>
            </div>
            <div class="strategy-list">
              <span v-for="tag in selectedPost.template.strategy_labels" :key="tag" class="strategy-tag">
                {{ tag }}
              </span>
            </div>
          </div>

          <div class="tasks-overview-card">
            <h4><el-icon><Filter /></el-icon> 任务预览</h4>
            <div class="items-list">
              <div v-for="item in previewItems" :key="item.item_id || item.order" class="item-node">
                <div class="node-idx">{{ String(item.order).padStart(2, '0') }}</div>
                <div class="node-content">{{ item.content }}</div>
              </div>
              <div v-if="previewItems.length === 0" class="item-node more">暂无任务预览</div>
              <div v-else-if="previewOverflowCount > 0" class="item-node more">
                ... 更多 {{ previewOverflowCount }} 个任务项 ...
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
                  <el-button type="primary" round size="default" :loading="commentSubmitting" @click="submitComment">
                    发布评论
                  </el-button>
                </div>
              </div>
            </div>

            <div class="comments-list">
              <div v-for="comment in comments" :key="comment.comment_id" class="comment-item">
                <img :src="comment.author.avatar_url" class="c-avatar" />
                <div class="c-body">
                  <div class="c-user">
                    {{ comment.author.nickname }}
                    <span class="c-time">{{ formatDate(comment.created_at) }}</span>
                  </div>
                  <div class="c-content">{{ comment.content }}</div>
                  <div class="c-actions">
                    <span v-if="comment.status === 'visible'" class="btn" @click="startReply(comment.comment_id)">回复</span>
                    <span v-if="comment.can_delete && comment.status === 'visible'" class="btn del" @click="handleDeleteComment(comment.comment_id)">
                      删除
                    </span>
                  </div>

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
                        <el-button size="small" link @click="resetReplyState">取消</el-button>
                        <el-button size="small" type="primary" round :loading="commentSubmitting" @click="submitReply(comment)">
                          提交回复
                        </el-button>
                      </div>
                    </div>
                  </transition>

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
                          <span v-if="child.status === 'visible'" class="btn" @click="startReply(child.comment_id)">回复</span>
                          <span v-if="child.can_delete && child.status === 'visible'" class="btn del" @click="handleDeleteComment(child.comment_id)">
                            删除
                          </span>
                        </div>

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
                              <el-button size="small" link @click="resetReplyState">取消</el-button>
                              <el-button size="small" type="primary" round :loading="commentSubmitting" @click="submitReply(child)">
                                提交回复
                              </el-button>
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
