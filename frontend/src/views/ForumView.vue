<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ChatDotRound, Connection, Plus, Search, Star } from '@element-plus/icons-vue'

import {
  createForumPost,
  importForumPost,
  likeForumPost,
  listForumPosts,
  listForumTags,
  unlikeForumPost,
} from '@/api/forum'
import { getTaskClassList } from '@/api/scheduleCenter'
import { useRouter } from 'vue-router'
import type { ForumPostBrief } from '@/types/forum'
import type { TaskClassListItem } from '@/types/schedule'
import { getAvatarUrl } from '@/utils/avatar'

const router = useRouter()

const allTagLabel = '全部'
const postPageSize = 60

const activeTag = ref(allTagLabel)
const searchQuery = ref('')
const sortBy = ref<'latest' | 'likes' | 'imports'>('latest')
const publishDialogVisible = ref(false)
const postLoading = ref(false)
const publishSubmitting = ref(false)
const posts = ref<ForumPostBrief[]>([])
const forumTags = ref<string[]>([])
const taskClasses = ref<TaskClassListItem[]>([])

const publishForm = ref({
  taskClassId: undefined as number | undefined,
  title: '',
  summary: '',
  tags: [] as string[],
})

const filteredPosts = computed(() => posts.value)
const tagOptions = computed(() => [allTagLabel, ...forumTags.value])
const publishTagOptions = computed(() => forumTags.value)

let searchTimer: ReturnType<typeof setTimeout> | null = null
let postRequestSerial = 0

function openDetails(post: ForumPostBrief) {
  router.push(`/forum/${post.post_id}`)
}

function resetPublishForm() {
  publishForm.value = {
    taskClassId: undefined,
    title: '',
    summary: '',
    tags: [],
  }
}

async function loadPosts() {
  const requestId = ++postRequestSerial
  postLoading.value = true

  try {
    const page = await listForumPosts({
      page: 1,
      page_size: postPageSize,
      sort: sortBy.value,
      keyword: searchQuery.value.trim(),
      tag: activeTag.value === allTagLabel ? '' : activeTag.value,
    })

    if (requestId !== postRequestSerial) {
      return
    }

    posts.value = page.items ?? []
  } catch (error) {
    if (requestId === postRequestSerial) {
      ElMessage.error(error instanceof Error ? error.message : '计划广场列表加载失败，请稍后重试')
    }
  } finally {
    if (requestId === postRequestSerial) {
      postLoading.value = false
    }
  }
}

async function loadTags() {
  try {
    const items = await listForumTags(20)
    forumTags.value = items.map((item) => item.tag).filter((tag) => tag.trim().length > 0)

    if (activeTag.value !== allTagLabel && !forumTags.value.includes(activeTag.value)) {
      activeTag.value = allTagLabel
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '计划广场标签加载失败，请稍后重试')
  }
}

async function loadTaskClassesIfNeeded() {
  if (taskClasses.value.length > 0) {
    return
  }

  taskClasses.value = await getTaskClassList()
}

async function openPublishDialog() {
  publishDialogVisible.value = true

  try {
    await loadTaskClassesIfNeeded()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '任务类列表加载失败，请稍后重试')
  }
}

async function handleLike(post: ForumPostBrief) {
  try {
    const result = post.viewer_state.liked
      ? await unlikeForumPost(post.post_id)
      : await likeForumPost(post.post_id)

    post.viewer_state.liked = result.liked
    post.counters.like_count = result.like_count
    ElMessage.success(result.liked ? '点赞成功' : '已取消点赞')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '点赞操作失败，请稍后重试')
  }
}

async function handleImport(post: ForumPostBrief) {
  if (post.viewer_state.imported_once) {
    ElMessage.info('这个计划已经导入过啦')
    return
  }

  try {
    await ElMessageBox.confirm(`确定要将《${post.title}》导入到你的计划中吗？`, '导入确认', {
      confirmButtonText: '立即导入',
      cancelButtonText: '取消',
      type: 'info',
    })

    const result = await importForumPost(post.post_id, post.title)
    post.viewer_state.imported_once = true
    post.counters.import_count = result.import_count

    ElMessage.success(`导入成功，已创建任务类《${result.task_class_title}》`)
  } catch (error) {
    if (error === 'cancel' || error === 'close') {
      return
    }
    ElMessage.error(error instanceof Error ? error.message : '导入计划失败，请稍后重试')
  }
}

async function handlePublishPost() {
  const taskClassId = publishForm.value.taskClassId
  const title = publishForm.value.title.trim()
  const summary = publishForm.value.summary.trim()
  const tags = publishForm.value.tags.map((tag) => tag.trim()).filter((tag) => tag.length > 0)

  if (!taskClassId) {
    ElMessage.warning('请先选择要发布的任务类')
    return
  }
  if (title.length < 4 || title.length > 40) {
    ElMessage.warning('标题长度需要保持在 4 到 40 个字符之间')
    return
  }
  if (tags.length === 0) {
    ElMessage.warning('请至少选择 1 个标签')
    return
  }
  if (tags.length > 5) {
    ElMessage.warning('标签最多选择 5 个')
    return
  }

  publishSubmitting.value = true

  try {
    await createForumPost({
      task_class_id: taskClassId,
      title,
      summary,
      tags,
    })

    publishDialogVisible.value = false
    resetPublishForm()
    ElMessage.success('发布成功，已经同步到计划广场')
    await Promise.all([loadTags(), loadPosts()])
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '发布计划失败，请稍后重试')
  } finally {
    publishSubmitting.value = false
  }
}

watch([activeTag, sortBy], () => {
  void loadPosts()
})

watch(searchQuery, () => {
  if (searchTimer) {
    clearTimeout(searchTimer)
  }

  // 1. 搜索输入先做短暂防抖，避免每个字符都直接触发一次列表请求。
  // 2. 防抖结束后只保留最后一次关键词，保证界面和用户最后输入保持一致。
  searchTimer = setTimeout(() => {
    void loadPosts()
  }, 250)
})

onMounted(async () => {
  await Promise.all([loadTags(), loadPosts()])
})

onBeforeUnmount(() => {
  if (searchTimer) {
    clearTimeout(searchTimer)
  }
})
</script>
<template>
  <div class="forum-container">
    <header class="forum-header">
      <div class="header-left">
        <h1>计划广场</h1>
        <p>发现并分享优质的任务计划模板</p>
      </div>

      <div class="header-actions">
        <div class="search-box">
          <el-input
            v-model="searchQuery"
            class="search-input"
            placeholder="搜索计划、关键词..."
            :prefix-icon="Search"
            clearable
          />
        </div>
        <el-button type="primary" :icon="Plus" round @click="openPublishDialog">
          发布计划
        </el-button>
      </div>
    </header>

    <div class="forum-filters">
      <div class="tags-scroller">
        <button
          v-for="tag in tagOptions"
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

    <main class="forum-grid" v-loading="postLoading">
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
              <img :src="getAvatarUrl(post.author.avatar_url, post.author.user_id)" class="author-avatar" />
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

      <div v-if="!postLoading && filteredPosts.length === 0" class="empty-state">
        <el-empty description="暂无符合条件的计划" />
      </div>
    </main>

    <el-dialog
      v-model="publishDialogVisible"
      title="发布新计划"
      width="500px"
      append-to-body
      @closed="resetPublishForm"
    >
      <el-form label-position="top">
        <el-form-item label="选择现有计划模板" required>
          <el-select v-model="publishForm.taskClassId" placeholder="请选择你的 TaskClass" style="width: 100%" filterable>
            <el-option
              v-for="taskClass in taskClasses"
              :key="taskClass.id"
              :label="taskClass.name"
              :value="taskClass.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="标题" required>
          <el-input v-model="publishForm.title" placeholder="给你的计划起个吸引人的名字(4-40字)" />
        </el-form-item>
        <el-form-item label="简介">
          <el-input
            v-model="publishForm.summary"
            type="textarea"
            :rows="3"
            placeholder="详细描述一下这个计划的适用人群和优势..."
          />
        </el-form-item>
        <el-form-item label="标签" required>
          <el-select
            v-model="publishForm.tags"
            multiple
            filterable
            allow-create
            default-first-option
            placeholder="请至少选择 1 个标签（最多 5 个）"
          >
            <el-option v-for="tag in publishTagOptions" :key="tag" :label="tag" :value="tag" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="publishDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="publishSubmitting" @click="handlePublishPost">
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
  width: 320px;
}

.search-input :deep(.el-input__wrapper) {
  background-color: #f1f5f9 !important;
  box-shadow: none !important;
  border: 1px solid transparent !important;
  border-radius: 12px !important;
  padding: 4px 14px !important;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1) !important;
}

.search-input :deep(.el-input__wrapper.is-focus) {
  background-color: #fff !important;
  border-color: #3b82f6 !important;
  box-shadow: 0 0 0 4px rgba(59, 130, 246, 0.1) !important;
}

.search-input :deep(.el-input__inner) {
  background-color: transparent !important;
  height: 36px !important;
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
:deep(.el-input__wrapper) {
  border-radius: 12px !important;
  background-color: #f1f5f9 !important;
  box-shadow: none !important;
  border: 1px solid transparent !important;
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1) !important;
}

:deep(.el-input__wrapper.is-focus) {
  background-color: #fff !important;
  border-color: #3b82f6 !important;
  box-shadow: 0 0 0 4px rgba(59, 130, 246, 0.1) !important;
}

:deep(.el-input__inner) {
  background: transparent !important;
  border: none !important;
  color: #1e293b !important;
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

.forum-filters {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 20px;
  padding: 20px 40px; /* Added vertical padding */
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

.sort-dropdown :deep(.el-input__wrapper) {
  background-color: #fff !important;
  border: 1px solid #e2e8f0 !important;
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

.stat-item.imported {
  color: #10b981;
}

.empty-state {
  grid-column: 1 / -1;
  padding: 80px 0;
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
