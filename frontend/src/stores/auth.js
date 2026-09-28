import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { authAPI, clearSession } from '@/api/client'
import { apiErrorMessage } from '@/utils/apiError'

export const useAuthStore = defineStore('auth', () => {
  const user = ref(JSON.parse(localStorage.getItem('user') || 'null'))
  const token = ref(localStorage.getItem('token'))

  const isAuthenticated = computed(() => !!token.value)

  const avatarUrl = computed(() => {
    if (user.value?.avatar_path) {
      return '/' + user.value.avatar_path
    }
    return null
  })

  async function login(credentials) {
    try {
      const { data } = await authAPI.login(credentials)
      user.value = data.user
      token.value = data.token
      localStorage.setItem('token', data.token)
      localStorage.setItem('user', JSON.stringify(data.user))
      return data
    } catch (error) {
      throw new Error(apiErrorMessage(error), { cause: error })
    }
  }

  async function register(userData) {
    try {
      const { data } = await authAPI.register(userData)
      user.value = data.user
      token.value = data.token
      localStorage.setItem('token', data.token)
      localStorage.setItem('user', JSON.stringify(data.user))
      return data
    } catch (error) {
      throw new Error(apiErrorMessage(error), { cause: error })
    }
  }

  function updateUser(updatedUser) {
    user.value = updatedUser
    localStorage.setItem('user', JSON.stringify(updatedUser))
  }

  // Replaces the access token, e.g. after a password change rotated the
  // session (the server set a fresh refresh cookie in the same response).
  function setToken(newToken) {
    token.value = newToken
    localStorage.setItem('token', newToken)
  }

  function logout() {
    // Ask the server to expire the HttpOnly refresh cookie; the local session
    // is dropped regardless of whether that request succeeds.
    authAPI.logout().catch(() => {})
    user.value = null
    token.value = null
    clearSession()
  }

  return { user, token, isAuthenticated, avatarUrl, login, register, updateUser, setToken, logout }
})
