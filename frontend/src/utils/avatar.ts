/**
 * 获取头像 URL，支持默认兜底
 * @param url 原始头像 URL
 * @param seed 兜底种（如用户 ID 或昵称），确保同一用户的默认头像一致
 * @returns 最终可用的头像 URL
 */
export function getAvatarUrl(url?: string, seed?: string | number): string {
  if (url && url.trim() && (url.startsWith('http') || url.startsWith('/') || url.startsWith('data:'))) {
    return url
  }

  // 使用 DiceBear 提供的 avataaars 系列作为默认头像
  // 如果没有提供 seed，则生成一个随机字符串
  const avatarSeed = seed !== undefined && seed !== null ? String(seed) : 'default'
  return `https://api.dicebear.com/7.x/avataaars/svg?seed=${encodeURIComponent(avatarSeed)}`
}
