import axios from 'axios'

interface ErrorBody {
  info?: string
  message?: string
}

export function extractErrorMessage(error: unknown, fallback: string) {
  if (axios.isAxiosError<ErrorBody>(error)) {
    const responseInfo = error.response?.data?.info
    const responseMessage = error.response?.data?.message
    return responseInfo || responseMessage || error.message || fallback
  }

  if (error instanceof Error) {
    return error.message
  }

  return fallback
}
