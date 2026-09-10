<template>
  <main class="login-layout first-login-password">
    <section class="auth-panel" aria-labelledby="password-title" :aria-busy="submitting || policyLoading">
      <header class="auth-header">
        <p class="product-name">{{ PRODUCT_NAME }}</p>
        <h1 id="password-title">{{ t('auth.firstLoginPassword.title') }}</h1>
        <p class="auth-description">{{ t('auth.firstLoginPassword.description') }}</p>
        <p v-if="authStore.user?.email" class="account">{{ authStore.user.email }}</p>
      </header>

      <div v-if="policyLoading" class="policy-state invite-banner" role="status">
        <t-loading size="small" /> {{ t('auth.firstLoginPassword.loadingPolicy') }}
      </div>
      <div v-else-if="policyError" class="policy-state invite-banner invite-banner--error" role="alert">
        {{ t('auth.firstLoginPassword.policyError') }}
        <button type="button" class="text-link" @click="loadPolicy">{{ t('auth.workspaceOnboarding.retry') }}</button>
      </div>

      <t-form ref="formRef" :data="form" :rules="rules" layout="vertical" label-align="top"
        :required-mark="false" class="auth-form" @submit="submit">
        <t-form-item name="oldPassword" :label="t('userProfile.changePassword.currentLabel')" for="current-password">
          <t-input v-input-id="'current-password'" v-model="form.oldPassword" type="password" autocomplete="current-password"
            :disabled="submitting" :placeholder="t('userProfile.changePassword.currentPlaceholder')" autofocus />
        </t-form-item>
        <t-form-item name="newPassword" :label="t('userProfile.changePassword.newLabel')" for="new-password">
          <t-input v-input-id="'new-password'" v-model="form.newPassword" type="password" autocomplete="new-password"
            :disabled="submitting" :placeholder="t('userProfile.changePassword.newPlaceholder')" />
        </t-form-item>
        <t-form-item name="confirmPassword" :label="t('userProfile.changePassword.confirmLabel')" for="confirm-password">
          <t-input v-input-id="'confirm-password'" v-model="form.confirmPassword" type="password" autocomplete="new-password"
            :disabled="submitting" :placeholder="t('userProfile.changePassword.confirmPlaceholder')" />
        </t-form-item>
        <p v-if="error" class="form-error" role="alert">{{ error }}</p>
        <t-button class="submit-button" theme="primary" type="submit" block :loading="submitting"
          :disabled="policyLoading || policyError">
          {{ t('userProfile.changePassword.submit') }}
        </t-button>
      </t-form>
      <div class="auth-switch logout">
        <button type="button" class="text-link" :disabled="submitting" @click="leave">
          {{ t('auth.firstLoginPassword.otherAccount') }}
        </button>
      </div>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { MessagePlugin, type FormInstanceFunctions, type FormRule } from 'tdesign-vue-next'
import { changePassword, getAuthConfig, logout } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { newPasswordRules } from '@/utils/passwordPolicy'
import { PRODUCT_NAME } from '@/config/brand'
import { vInputId } from '@/directives/inputId'

const { t } = useI18n()
const router = useRouter()
const authStore = useAuthStore()
const formRef = ref<FormInstanceFunctions | null>(null)
const form = reactive({ oldPassword: '', newPassword: '', confirmPassword: '' })
const submitting = ref(false)
const policyLoading = ref(true)
const policyError = ref(false)
const complexPassword = ref(false)
const error = ref('')
const rules = computed<Record<string, FormRule[]>>(() => ({
  oldPassword: [{ required: true, message: t('userProfile.changePassword.currentRequired'), type: 'error' }],
  newPassword: newPasswordRules(t, complexPassword.value, [{
    validator: (value: string) => value !== form.oldPassword,
    message: t('userProfile.changePassword.sameAsCurrent'), type: 'error',
  }]),
  confirmPassword: [
    { required: true, message: t('auth.confirmPasswordRequired'), type: 'error' },
    { validator: (value: string) => value === form.newPassword, message: t('auth.passwordMismatch'), type: 'error' },
  ],
}))

async function loadPolicy() {
  policyLoading.value = true
  policyError.value = false
  try {
    const config = await getAuthConfig()
    if (!config.success) throw new Error('Password policy unavailable')
    complexPassword.value = config.complex_password_enabled === true
  } catch {
    policyError.value = true
  } finally {
    policyLoading.value = false
  }
}

async function submit() {
  if (submitting.value || policyLoading.value || policyError.value) return
  if (await formRef.value?.validate() !== true) return
  submitting.value = true
  error.value = ''
  try {
    const response = await changePassword({ old_password: form.oldPassword, new_password: form.newPassword })
    if (!response.success) {
      error.value = response.message || t('userProfile.changePassword.failed')
      return
    }
    // Password rotation revokes all sessions on the server.
    form.oldPassword = form.newPassword = form.confirmPassword = ''
    const invite = sessionStorage.getItem('weknora_pending_invite_token')
    sessionStorage.removeItem('weknora_pending_invite_token')
    authStore.logout()
    MessagePlugin.success(t('userProfile.changePassword.success'))
    await router.replace({ path: '/login', query: invite ? { token: invite } : undefined })
  } catch (cause: any) {
    error.value = cause?.message || t('userProfile.changePassword.failed')
  } finally {
    submitting.value = false
  }
}

async function leave() {
  try {
    await logout()
  } catch {
    // Local sign-out must remain available when the session has expired.
  } finally {
    authStore.logout()
    await router.replace('/login')
  }
}

onMounted(loadPolicy)
</script>

<style scoped lang="less" src="./auth.less"></style>
<style scoped lang="less">
.product-name { margin: 0 0 12px; color: var(--auth-muted); font-size: 13px; font-weight: 500; line-height: 1.5; }
.account { max-width: 100%; margin: 12px 0 0; font-size: 13px; line-height: 1.7; overflow-wrap: anywhere; }
.policy-state { align-items: center; flex-wrap: wrap; }
.form-error { margin: 0 0 20px; color: var(--td-error-color); font-size: 13px; line-height: 1.6; overflow-wrap: anywhere; }
.logout { margin-top: 24px; }
</style>
