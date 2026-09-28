<template>
  <BaseModal :show="true" :title="account ? t('ledger.accountModal.editTitle') : t('ledger.accountModal.createTitle')" @close="$emit('close')">
    <form class="space-y-4" @submit.prevent="save">
      <Input
        id="account-name"
        v-model="form.name"
        :label="t('ledger.accountModal.name')"
        :placeholder="t('ledger.accountModal.namePlaceholder')"
        required
      />
      <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <Input
          id="account-bank"
          v-model="form.bank"
          :label="t('ledger.accountModal.bank')"
          :placeholder="t('ledger.accountModal.bankPlaceholder')"
        />
        <div>
          <label for="account-type" class="block text-sm text-ink-soft mb-1">{{ t('ledger.accountModal.type') }}</label>
          <select id="account-type" v-model="form.type" :class="selectClass">
            <option v-for="type in ACCOUNT_TYPES" :key="type" :value="type">{{ t(`ledger.types.${type}`) }}</option>
          </select>
        </div>
      </div>
      <Input
        id="account-number"
        v-model="form.account_number"
        :label="t('ledger.accountModal.number')"
        inputmode="numeric"
        autocomplete="off"
      />
      <div>
        <Input
          id="account-opening"
          v-model="form.opening_balance"
          :label="t('ledger.accountModal.opening')"
          type="text"
          inputmode="decimal"
          autocomplete="off"
        />
        <p class="mt-1 text-xs text-ink-muted">{{ t('ledger.accountModal.openingHint') }}</p>
      </div>
      <div>
        <label for="account-owner" class="block text-sm text-ink-soft mb-1">{{ t('ledger.accountModal.owner') }}</label>
        <select id="account-owner" v-model="form.owner_member_id" :class="selectClass">
          <option :value="0">{{ t('ledger.accountModal.ownerNone') }}</option>
          <option v-for="m in ledger.members" :key="m.id" :value="m.id">{{ m.name }}</option>
        </select>
      </div>
      <label v-if="account" class="flex items-center gap-2 text-sm text-ink-soft">
        <input id="account-archived" v-model="form.is_archived" type="checkbox" class="w-4 h-4 rounded border-line" />
        {{ t('ledger.accountModal.archive') }}
      </label>

      <p v-if="error" class="text-sm text-danger" role="alert">{{ error }}</p>

      <div class="flex gap-2 pt-2">
        <Button v-if="account" variant="danger" :disabled="saving" @click="remove">{{ t('ledger.accountModal.delete') }}</Button>
        <div class="flex-1" />
        <Button variant="secondary" @click="$emit('close')">{{ t('ledger.accountModal.cancel') }}</Button>
        <Button type="submit" :disabled="saving">{{ t('ledger.accountModal.save') }}</Button>
      </div>
    </form>
  </BaseModal>
</template>

<script setup>
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ledgerAPI } from '@/api/client'
import { useLedgerStore } from '@/stores/ledger'
import { useConfirm } from '@/composables/useConfirm'
import { apiErrorMessage } from '@/utils/apiError'
import BaseModal from '@/components/common/BaseModal.vue'
import Input from '@/components/common/Input.vue'
import Button from '@/components/common/Button.vue'

const props = defineProps({ account: { type: Object, default: null } })
const emit = defineEmits(['close', 'saved'])
const { t } = useI18n()
const ledger = useLedgerStore()
const { confirm } = useConfirm()

const ACCOUNT_TYPES = ['savings', 'current', 'fixed', 'credit_card', 'cash', 'ewallet']
const selectClass = 'w-full px-3 py-3 border border-line rounded-lg bg-surface text-ink text-base focus:outline-none focus:ring-2 focus:ring-blue-500'

const a = props.account
const form = ref({
  name: a?.name || '',
  bank: a?.bank || '',
  type: a?.type || 'savings',
  account_number: a?.account_number || '',
  opening_balance: a ? String(a.opening_balance) : '',
  owner_member_id: a?.owner_member_id || 0,
  is_archived: !!a?.is_archived,
})
const saving = ref(false)
const error = ref(null)

async function save() {
  error.value = null
  const opening = parseFloat(String(form.value.opening_balance || '0').replace(/,/g, ''))
  const payload = {
    name: form.value.name,
    bank: form.value.bank,
    type: form.value.type,
    account_number: form.value.account_number.replace(/\s|-/g, ''),
    opening_balance: Number.isFinite(opening) ? opening : 0,
    owner_member_id: form.value.owner_member_id || 0,
    is_archived: form.value.is_archived,
  }
  saving.value = true
  try {
    if (a) await ledgerAPI.updateAccount(a.id, payload)
    else await ledgerAPI.createAccount(ledger.propertyId, payload)
    await ledger.refreshAccounts()
    window.$toast?.success(t('ledger.saved'))
    emit('saved')
    emit('close')
  } catch (err) {
    error.value = apiErrorMessage(err)
  } finally {
    saving.value = false
  }
}

async function remove() {
  const ok = await confirm({
    title: t('ledger.accountModal.delete'),
    message: a.name,
    confirmText: t('ledger.delete'),
    variant: 'danger',
  })
  if (!ok) return
  saving.value = true
  try {
    await ledgerAPI.deleteAccount(a.id)
    await ledger.refreshAccounts()
    emit('saved')
    emit('close')
  } catch (err) {
    error.value = apiErrorMessage(err)
  } finally {
    saving.value = false
  }
}
</script>
