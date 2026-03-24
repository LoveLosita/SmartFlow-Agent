const weekdayMap = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六']

function toDate(value?: string | null) {
  if (!value) {
    return null
  }

  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

export function formatHeaderDate(date = new Date()) {
  const year = date.getFullYear()
  const month = `${date.getMonth() + 1}`.padStart(2, '0')
  const day = `${date.getDate()}`.padStart(2, '0')
  return `${year}年${month}月${day}日 ${weekdayMap[date.getDay()]}`
}

export function formatTimeRange(start?: string | null, end?: string | null) {
  if (!start || !end) {
    return '待安排'
  }
  return `${start} - ${end}`
}

export function formatDeadline(deadline?: string | null) {
  const date = toDate(deadline)
  if (!date) {
    return '未设置截止时间'
  }

  const month = `${date.getMonth() + 1}`.padStart(2, '0')
  const day = `${date.getDate()}`.padStart(2, '0')
  const hour = `${date.getHours()}`.padStart(2, '0')
  const minute = `${date.getMinutes()}`.padStart(2, '0')
  return `${month}-${day} ${hour}:${minute}`
}

export function formatConversationTime(value?: string | null) {
  const date = toDate(value)
  if (!date) {
    return '刚刚'
  }

  const now = new Date()
  const sameDay =
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate()

  const hour = `${date.getHours()}`.padStart(2, '0')
  const minute = `${date.getMinutes()}`.padStart(2, '0')
  if (sameDay) {
    return `${hour}:${minute}`
  }

  const month = `${date.getMonth() + 1}`.padStart(2, '0')
  const day = `${date.getDate()}`.padStart(2, '0')
  return `${month}-${day} ${hour}:${minute}`
}

export function formatMessageTime(value?: string | null) {
  const date = toDate(value)
  if (!date) {
    return ''
  }

  const hour = `${date.getHours()}`.padStart(2, '0')
  const minute = `${date.getMinutes()}`.padStart(2, '0')
  return `${hour}:${minute}`
}
