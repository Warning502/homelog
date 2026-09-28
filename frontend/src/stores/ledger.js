import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import apiClient, { ledgerAPI } from '@/api/client'

// Money accounts and household members of the current property, shared by the
// Accounts page and the expense forms (which offer "paid from account" and
// "spent on" pickers).
export const useLedgerStore = defineStore('ledger', () => {
  const propertyId = ref(null)
  const accounts = ref([])
  const members = ref([])
  const loaded = ref(false)
  let loading = null

  const activeAccounts = computed(() => accounts.value.filter(a => !a.is_archived))

  async function resolveProperty() {
    if (propertyId.value) return propertyId.value
    const { data } = await apiClient.get('/properties')
    const current = (data || []).find(p => p.is_current) || (data || [])[0]
    propertyId.value = current?.id ?? null
    return propertyId.value
  }

  async function load(force = false) {
    if (loaded.value && !force) return
    if (loading && !force) return loading
    loading = (async () => {
      const pid = await resolveProperty()
      if (!pid) return
      const [acc, mem] = await Promise.all([
        ledgerAPI.accounts(pid),
        apiClient.get(`/properties/${pid}/members`),
      ])
      accounts.value = acc.data || []
      members.value = mem.data || []
      loaded.value = true
    })()
    try {
      await loading
    } finally {
      loading = null
    }
  }

  async function refreshAccounts() {
    if (!propertyId.value) return load(true)
    const { data } = await ledgerAPI.accounts(propertyId.value)
    accounts.value = data || []
  }

  function accountName(id) {
    const a = accounts.value.find(x => x.id === id)
    return a ? (a.bank ? `${a.bank} · ${a.name}` : a.name) : ''
  }

  function memberName(id) {
    return members.value.find(m => m.id === id)?.name || ''
  }

  function reset() {
    propertyId.value = null
    accounts.value = []
    members.value = []
    loaded.value = false
  }

  return { propertyId, accounts, activeAccounts, members, loaded, load, refreshAccounts, accountName, memberName, reset }
})
