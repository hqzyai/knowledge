<template>
  <main class="login-layout">
    <div class="language-switch">
      <t-icon name="internet" size="16px" aria-hidden="true" />
      <select :value="locale" :aria-label="$t('language.selectLanguage')"
        @change="selectLanguage(($event.target as HTMLSelectElement).value)">
        <option v-for="lang in languageOptions" :key="lang.value" :value="lang.value">{{ lang.label }}</option>
      </select>
    </div>

    <section class="auth-panel" aria-labelledby="auth-title" :aria-busy="loading || oidcLoading || inviteLookupLoading">
      <header class="auth-header">
        <h1 id="auth-title">{{ PRODUCT_NAME }}</h1>
        <p class="auth-description">{{ $t(isRegisterMode ? 'auth.registerSubtitle' : 'auth.subtitle') }}</p>
        <p v-if="!isRegisterMode && registrationEnabled" class="auth-switch">
          <span>{{ $t('auth.firstTime') }}</span>
          <button type="button" class="text-link" :disabled="isBusy" @click="toggleMode">{{ $t('auth.createAccount') }}</button>
        </p>
        <p v-else-if="isRegisterMode" class="auth-switch">
          <span>{{ $t('auth.haveAccount') }}</span>
          <button type="button" class="text-link" :disabled="isBusy" @click="toggleMode">{{ $t('auth.backToLogin') }}</button>
        </p>
      </header>

      <div v-if="inviteLookupLoading" class="invite-banner" role="status">
        <t-loading size="small" />
        <span>{{ $t('inviteRegister.loading') }}</span>
      </div>
      <div v-else-if="inviteLookup" class="invite-banner" role="status">
        <t-icon name="link" size="18px" aria-hidden="true" />
        <div>
          <p class="invite-title">{{ $t('inviteRegister.bannerTitle', { tenant: inviteLookup.tenant_name || '' }) }}</p>
          <p>{{ $t(isRegisterMode ? 'inviteRegister.bannerHint' : 'inviteRegister.bannerHintLogin') }}</p>
        </div>
      </div>
      <div v-else-if="inviteLookupError" class="invite-banner invite-banner--error" role="alert">
        <t-icon name="error-circle" size="18px" aria-hidden="true" />
        <p>{{ inviteLookupError }}</p>
      </div>

      <t-form v-if="!isRegisterMode" ref="formRef" :data="formData" :rules="formRules"
        @submit="handleLogin" layout="vertical" label-align="top" :required-mark="false" class="auth-form">
        <t-form-item :label="$t('auth.email')" name="email" for="login-email">
          <t-input v-input-id="'login-email'" v-model="formData.email" :placeholder="$t('auth.emailPlaceholder')"
            type="text" autocomplete="email" :disabled="isBusy" />
        </t-form-item>
        <t-form-item :label="$t('auth.password')" name="password" for="login-password">
          <t-input v-input-id="'login-password'" v-model="formData.password" :placeholder="$t('auth.passwordPlaceholder')"
            type="password" autocomplete="current-password" :disabled="isBusy" />
        </t-form-item>
        <t-button type="submit" theme="primary" block :loading="loading" :disabled="oidcLoading || inviteLookupLoading"
          class="submit-button">{{ loading ? $t('auth.loggingIn') : $t('auth.login') }}</t-button>
        <template v-if="oidcEnabled">
          <div class="auth-divider"><span>{{ $t('auth.orContinueWith') }}</span></div>
          <t-button type="button" theme="default" variant="outline" block :loading="oidcLoading"
            :disabled="loading || inviteLookupLoading" class="oidc-button" @click="handleOIDCLogin">
            <template #icon><t-icon name="secured" aria-hidden="true" /></template>
            {{ oidcLoading ? $t('auth.redirectingToOIDC') : oidcLoginText }}
          </t-button>
        </template>
      </t-form>

      <t-form v-else-if="registrationEnabled || inviteLookup" ref="registerFormRef" :data="registerData"
        :rules="registerRules" @submit="handleRegister" layout="vertical" label-align="top" :required-mark="false" class="auth-form">
        <t-form-item :label="$t('auth.username')" name="username" for="register-username">
          <t-input v-input-id="'register-username'" v-model="registerData.username" :placeholder="$t('auth.usernamePlaceholder')"
            autocomplete="username" :disabled="isBusy" />
        </t-form-item>
        <t-form-item :label="$t('auth.email')" name="email" for="register-email">
          <t-input v-input-id="'register-email'" v-model="registerData.email" :placeholder="$t('auth.emailPlaceholder')"
            type="text" autocomplete="email" :disabled="isBusy" />
        </t-form-item>
        <t-form-item :label="$t('auth.password')" name="password" for="register-password">
          <t-input v-input-id="'register-password'" v-model="registerData.password" :placeholder="$t('auth.passwordPlaceholder')"
            type="password" autocomplete="new-password" :disabled="isBusy" />
        </t-form-item>
        <t-form-item :label="$t('auth.confirmPassword')" name="confirmPassword" for="register-confirm-password">
          <t-input v-input-id="'register-confirm-password'" v-model="registerData.confirmPassword" :placeholder="$t('auth.confirmPasswordPlaceholder')"
            type="password" autocomplete="new-password" :disabled="isBusy" />
        </t-form-item>
        <t-button type="submit" theme="primary" block :loading="loading" :disabled="oidcLoading || inviteLookupLoading"
          class="submit-button">{{ loading ? $t('auth.registering') : $t('auth.createAccount') }}</t-button>
      </t-form>
    </section>
  </main>
</template>

<script setup lang="ts">
import { ref, reactive, nextTick, onMounted, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { useRoleLabel } from '@/composables/useRoleLabel'
import { notifyLoginSuccess } from '@/utils/loginNotify'
import { newPasswordRules } from '@/utils/passwordPolicy'
import {
  login,
  register,
  getOIDCAuthorizationURL,
  getOIDCConfig,
  autoSetup,
  getAuthConfig,
  userInfoFromApi,
  getInvitationByToken,
  registerByInvite,
  type InviteLookup,
} from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'

import { PRODUCT_NAME } from '@/config/brand'
import { vInputId } from '@/directives/inputId'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const { t, tm, locale } = useI18n()
const { formatRole, roleIcon } = useRoleLabel()

// Form references
const formRef = ref()
const registerFormRef = ref()

// State management
const loading = ref(false)
const oidcLoading = ref(false)
const isRegisterMode = ref(false)
const oidcEnabled = ref(false)
const oidcProviderName = ref('')
// registrationEnabled defaults to true so that on first paint the Register
// link is visible; the actual mode is fetched from /auth/config in onMounted.
// In invite_only mode the link/card are hidden.
const registrationEnabled = ref(true)
const complexPasswordEnabled = ref(false)

// invite-link state. When the URL carries ?token=xxx we resolve it to
// the originating tenant + role and switch the form into a "register
// via invitation" mode. The token bypasses the normal invite_only
// gate — possessing it IS the authorisation. Submitting the register
// form with this set hits /auth/register-by-invite (auto-login on
// success) instead of /auth/register.
const inviteToken = ref('')
const inviteLookup = ref<InviteLookup | null>(null)
const inviteLookupError = ref('')
const inviteLookupLoading = ref(false)

// Language options
const languageOptions = [
  { value: 'zh-CN', label: '简体中文' },
  { value: 'en-US', label: 'English' },
  { value: 'ru-RU', label: 'Русский' },
  { value: 'ko-KR', label: '한국어' }
]

const oidcLoginText = computed(() => {
  if (oidcProviderName.value) {
    return t('auth.oidcLoginWithProvider', { provider: oidcProviderName.value })
  }
  return t('auth.oidcLogin')
})
const isBusy = computed(() => loading.value || oidcLoading.value || inviteLookupLoading.value)

// Login form data
const formData = reactive<{ [key: string]: any }>({
  email: '',
  password: '',
})

// Register form data
const registerData = reactive<{ [key: string]: any }>({
  username: '',
  email: '',
  password: '',
  confirmPassword: ''
})

// Login form validation rules
const formRules = computed(() => ({
  email: [
    { required: true, message: t('auth.emailRequired'), type: 'error' },
    { email: true, message: t('auth.emailInvalid'), type: 'error' }
  ],
  password: [
    { required: true, message: t('auth.passwordRequired'), type: 'error' },
    { min: 8, message: t('auth.passwordMinLength'), type: 'error' },
    { max: 32, message: t('auth.passwordMaxLength'), type: 'error' }
  ],
}))

// Register form validation rules
const registerRules = computed(() => ({
  username: [
    { required: true, message: t('auth.usernameRequired'), type: 'error' },
    { min: 2, message: t('auth.usernameMinLength'), type: 'error' },
    { max: 20, message: t('auth.usernameMaxLength'), type: 'error' },
    {
      pattern: /^[a-zA-Z0-9_\u4e00-\u9fa5]+$/,
      message: t('auth.usernameInvalid'),
      type: 'error'
    }
  ],
  email: [
    { required: true, message: t('auth.emailRequired'), type: 'error' },
    { email: true, message: t('auth.emailInvalid'), type: 'error' }
  ],
  password: newPasswordRules(t, complexPasswordEnabled.value),
  confirmPassword: [
    { required: true, message: t('auth.confirmPasswordRequired'), type: 'error' },
    {
      validator: (val: string) => val === registerData.password,
      message: t('auth.passwordMismatch'),
      type: 'error'
    }
  ]
}))

// Toggle login/register mode
const toggleMode = () => {
  isRegisterMode.value = !isRegisterMode.value

  Object.keys(registerData).forEach(key => {
    (registerData as any)[key] = ''
  })
}

// Native select keeps language switching accessible by keyboard.
const selectLanguage = (lang: string) => {
  locale.value = lang
  localStorage.setItem('locale', lang)
  formRef.value?.clearValidate()
  registerFormRef.value?.clearValidate()
  MessagePlugin.success(t('language.languageSaved'))
}

const persistLoginResponse = async (response: any, skipRedirect = false) => {
  // Backend renamed `tenant` to `active_tenant` and added `memberships`
  // when tenant-level RBAC landed (issue #1303). The two are otherwise
  // identical — `active_tenant` is the tenant whose ID is encoded in the
  // JWT, defaulting to the user's home tenant on a fresh login.
  const activeTenant = response.active_tenant || response.tenant
  if (response.user && response.token) {
    // user.tenant_id must be the user's HOME tenant (the immutable row
    // on the users table); useHomeTenant() and the home-badge logic both
    // assume so. The ACTIVE tenant (which can differ from home when the
    // server honoured a remembered last-active-tenant preference) is
    // expressed separately via setSelectedTenant below.
    const homeTenantIdRaw = response.user.tenant_id ?? activeTenant?.id ?? ''
    authStore.setUser(userInfoFromApi(response.user, homeTenantIdRaw))
    authStore.setToken(response.token)
    if (response.refresh_token) {
      authStore.setRefreshToken(response.refresh_token)
    }
    if (activeTenant) {
      authStore.setTenant({
        id: String(activeTenant.id) || '',
        name: activeTenant.name || '',
        owner_id: response.user.id || '',
        created_at: activeTenant.created_at || new Date().toISOString(),
        updated_at: activeTenant.updated_at || new Date().toISOString()
      })
    } else {
      authStore.setTenant(null)
    }
    if (Array.isArray(response.memberships)) {
      authStore.setMemberships(response.memberships)
    }
    // If the backend dropped us into a non-home tenant (honoured a
    // remembered "last active tenant" preference), set the override so
    // subsequent requests carry X-Tenant-ID and the UI stays consistent.
    // Otherwise clear any stale override left in localStorage by a
    // previous session for a different account.
    const activeIdNum = Number(activeTenant?.id)
    const homeIdNum = Number(homeTenantIdRaw)
    if (Number.isFinite(activeIdNum) && Number.isFinite(homeIdNum) && activeIdNum !== homeIdNum) {
      authStore.setSelectedTenant(activeIdNum, activeTenant?.name || null)
    } else {
      authStore.setSelectedTenant(null, null)
    }
  }

  // Pull runtime capabilities (including whether ordinary users may create
  // workspaces) before entering the main UI so create actions never flash
  // briefly when the deployment is invitation-only.
  await authStore.refreshFromAuthMe()
  await nextTick()
  if (skipRedirect) return
  router.replace(authStore.hasValidTenant ? '/platform/knowledge-bases' : '/onboarding/workspace')
}

const getBackendOIDCRedirectURI = () => `${window.location.origin}/api/v1/auth/oidc/callback`

const loadOIDCConfig = async () => {
  try {
    const response = await getOIDCConfig()
    oidcEnabled.value = !!response.success && !!response.enabled
    oidcProviderName.value = response.provider_display_name || ''
  } catch {
    oidcEnabled.value = false
    oidcProviderName.value = ''
  }
}

// loadAuthConfig fetches /auth/config and caches whether self-service
// registration is allowed. Failures fall back to "enabled" so a transient
// network glitch doesn't lock new users out of an open deployment.
const loadAuthConfig = async () => {
  try {
    const response = await getAuthConfig()
    registrationEnabled.value = response.registration_mode !== 'invite_only'
    complexPasswordEnabled.value = response.complex_password_enabled
  } catch {
    registrationEnabled.value = true
    complexPasswordEnabled.value = false
  }
}

const handleOIDCLogin = async () => {
  try {
    oidcLoading.value = true
    const response = await getOIDCAuthorizationURL(getBackendOIDCRedirectURI())
    const authorizationURL = response.authorization_url

    if (!response.success || !authorizationURL) {
      MessagePlugin.error(response.message || t('auth.oidcLoginFailed'))
      return
    }

    // 跳转 IdP 会丢失 URL 中的 token，暂存到 sessionStorage，回调后由 App.vue 兑换。
    if (inviteToken.value) {
      sessionStorage.setItem('weknora_pending_invite_token', inviteToken.value)
    }
    window.location.href = authorizationURL
  } catch (error: any) {
    console.error('OIDC 登录跳转失败:', error)
    MessagePlugin.error(error.message || t('auth.oidcLoginFailed'))
  } finally {
    oidcLoading.value = false
  }
}

// 用 token 加入空间并进入应用。会话此时已有效，故即便 token 失效也照常进入（避免困在登录页）。
const acceptAndEnter = async (token: string) => {
  loading.value = true
  try {
    const result = await authStore.acceptInvitationByTokenAndRefresh(token)
    if (result.ok) {
      MessagePlugin.success(t('inviteRegister.joined'))
    } else {
      MessagePlugin.warning(t('inviteRegister.invalidBody'))
    }
  } catch {
    MessagePlugin.warning(t('inviteRegister.invalidBody'))
  } finally {
    loading.value = false
    await nextTick()
    router.replace('/platform/knowledge-bases')
  }
}

// Handle login
const handleLogin = async () => {
  if (isBusy.value) return
  try {
    const valid = await formRef.value?.validate()
    if (valid !== true) return

    loading.value = true

    const response = await login({
      email: formData.email,
      password: formData.password,
    })

    if (response.success) {
      if (inviteToken.value) {
        // 从邀请链接登录：持久化会话后兑换 token 并进入对应空间。
        await persistLoginResponse(response, true)
        if (authStore.user?.preferences?.must_change_password === true) {
          sessionStorage.setItem('weknora_pending_invite_token', inviteToken.value)
          await router.replace('/change-password')
          return
        }
        await acceptAndEnter(inviteToken.value)
        return
      }
      await persistLoginResponse(response)
      notifyLoginSuccess(response, t, tm, formatRole, roleIcon)
    } else {
      MessagePlugin.error(response.message || t('auth.loginError'))
    }
  } catch (error: any) {
    console.error('登录错误:', error)
    MessagePlugin.error(error.message || t('auth.loginErrorRetry'))
  } finally {
    loading.value = false
  }
}

// Handle registration. Dispatches based on whether the user arrived
// with a share-link token: with token -> register-by-invite (auto-
// login on success); without -> the normal self-service register
// (drops back to the login form for the user to sign in).
const handleRegister = async () => {
  if (isBusy.value) return
  try {
    const valid = await registerFormRef.value?.validate()
    if (valid !== true) return

    loading.value = true

    if (inviteToken.value) {
      const response = await registerByInvite({
        token: inviteToken.value,
        username: registerData.username,
        email: registerData.email,
        password: registerData.password,
      })
      if (!response.success) {
        MessagePlugin.error(response.message || t('auth.registerFailed'))
        return
      }
      MessagePlugin.success(t('auth.registerSuccess'))
      // register-by-invite returns the same shape as login (token +
      // active_tenant + memberships), so reuse the login persistence
      // path — same store writes, same redirect target.
      await persistLoginResponse(response)
      return
    }

    const response = await register({
      username: registerData.username,
      email: registerData.email,
      password: registerData.password
    })

    if (response.success) {
      MessagePlugin.success(t('auth.registerSuccess'))

      // Switch to login mode and fill in email
      isRegisterMode.value = false
      formData.email = registerData.email

      // Clear register form
      Object.keys(registerData).forEach(key => {
        (registerData as any)[key] = ''
      })
    } else {
      MessagePlugin.error(response.message || t('auth.registerFailed'))
    }
  } catch (error: any) {
    console.error('注册错误:', error)
    MessagePlugin.error(error.message || t('auth.registerError'))
  } finally {
    loading.value = false
  }
}

// Check if already logged in; for lite edition, attempt transparent auto-setup
onMounted(async () => {
  // Share-link landing: ?token=xxx switches the form into invite-
  // register mode before any other auto-flow (logged-in redirect /
  // auto-setup / OIDC) gets a chance to redirect. Resolution failure
  // surfaces inline; the user can still log in normally if they
  // already have an account. We check this BEFORE the isLoggedIn
  // redirect so an existing session doesn't bounce the user to
  // /platform (and possibly back to /login if the session is stale),
  // dropping the invite token along the way.
  const tokenFromQuery = String(route.query.token || '').trim()
  if (tokenFromQuery) {
    inviteToken.value = tokenFromQuery
    inviteLookupLoading.value = true
    // 1. 先校验 token：无效/过期则停在登录页报错，不进注册模式。
    try {
      const resp = await getInvitationByToken(tokenFromQuery)
      if (resp.success && resp.data) {
        inviteLookup.value = resp.data
      } else {
        inviteLookupError.value = resp.message || t('inviteRegister.invalidBody')
        loadOIDCConfig()
        loadAuthConfig()
        return
      }
    } catch {
      inviteLookupError.value = t('inviteRegister.invalidBody')
      loadOIDCConfig()
      loadAuthConfig()
      return
    } finally {
      inviteLookupLoading.value = false
    }

    // 2. 已登录则直接兑换 token 进入空间（两种模式通用）。
    if (authStore.isLoggedIn && (await authStore.refreshFromAuthMe())) {
      await acceptAndEnter(tokenFromQuery)
      return
    }

    // 3. 未登录：按注册模式决定界面。invite_only 停在登录页、登录后再兑换；self_serve 保持注册流程。
    const cfg = await getAuthConfig()
    const inviteOnly = cfg.registration_mode === 'invite_only'
    registrationEnabled.value = !inviteOnly
    isRegisterMode.value = !inviteOnly
    loadOIDCConfig()
    return
  }

  if (authStore.isLoggedIn) {
    router.replace('/platform/knowledge-bases')
    return
  }

  const AUTO_SETUP_FAILED_KEY = 'weknora_auto_setup_failed'
  if (localStorage.getItem(AUTO_SETUP_FAILED_KEY) !== 'true') {
    try {
      const response = await autoSetup()
      if (response.success) {
        authStore.setLiteMode(true)
        await persistLoginResponse(response)
        return
      } else {
        localStorage.setItem(AUTO_SETUP_FAILED_KEY, 'true')
      }
    } catch {
      localStorage.setItem(AUTO_SETUP_FAILED_KEY, 'true')
    }
  }

  loadOIDCConfig()
  loadAuthConfig()
})
</script>

<style lang="less" scoped src="./auth.less"></style>
