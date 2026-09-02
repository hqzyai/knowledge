<template>
  <IntegrationLandingLayout
    :title="$t('integrations.claw.title')"
    :subtitle="$t('integrations.claw.subtitle')"
    variant="claw"
  >
    <template #actions>
      <div class="landing-action-grid">
        <IntegrationExternalCta
          variant="claw"
          trailing-icon="download"
          :disabled="downloading"
          :label="$t('integrations.claw.downloadCta')"
          :hint="$t('integrations.claw.downloadCtaHint')"
          @click="downloadSkill"
        >
          <template #icon>
            <t-icon name="folder-zip" size="18px" />
          </template>
        </IntegrationExternalCta>
        <IntegrationExternalCta
          variant="claw"
          :label="$t('integrations.claw.installCta')"
          :hint="$t('integrations.claw.installCtaHint')"
          @click="openClawHub"
        >
          <template #icon>
            <span class="ext-cta-emoji" role="img" :aria-label="$t('common.clawhubSkill')">🦞</span>
          </template>
        </IntegrationExternalCta>
      </div>
    </template>

    <template #main>
      <div class="landing-group">
        <section class="setting-drawer__section">
          <h4 class="setting-drawer__section-title">
            {{ $t('integrations.claw.capabilitiesTitle') }}
            <span class="section-head-extra">{{ capabilityKeys.length }}</span>
          </h4>
          <div class="capability-grid capability-grid--claw">
            <div v-for="key in capabilityKeys" :key="key" class="capability-card">
              <div class="capability-card__icon">
                <t-icon :name="capabilityIcons[key]" />
              </div>
              <h5 class="capability-card__title">{{ $t(`integrations.claw.capabilities.${key}.title`) }}</h5>
              <p class="capability-card__desc">{{ $t(`integrations.claw.capabilities.${key}.desc`) }}</p>
            </div>
          </div>
        </section>
      </div>
    </template>

    <template #aside>
      <div class="landing-group">
        <section class="setting-drawer__section">
          <h4 class="setting-drawer__section-title">{{ $t('integrations.claw.stepsTitle') }}</h4>
          <ol class="landing-steps">
            <li v-for="(step, index) in stepKeys" :key="step" class="landing-step">
              <span class="landing-step-num">{{ index + 1 }}</span>
              <div class="landing-step-body">
                <div class="landing-step-title">{{ $t(`integrations.claw.steps.${step}.title`) }}</div>
                <p class="landing-step-desc">{{ $t(`integrations.claw.steps.${step}.desc`) }}</p>
                <t-button
                  v-if="step === 'api'"
                  size="small"
                  variant="outline"
                  class="landing-step-action"
                  @click="openApiSettings"
                >
                  {{ $t('integrations.claw.openApiSettings') }}
                </t-button>
                <div v-if="step === 'env'" class="landing-step-embed">
                  <div class="code-toolbar">
                    <pre class="code-toolbar__code">{{ envExample }}</pre>
                    <t-button
                      class="code-toolbar__copy"
                      size="small"
                      variant="text"
                      shape="square"
                      :title="$t('integrations.claw.copy')"
                      @click="copyEnvExample"
                    >
                      <t-icon name="file-copy" size="16px" />
                    </t-button>
                  </div>
                </div>
                <div v-if="step === 'download'" class="landing-step-embed">
                  <div class="code-toolbar">
                    <pre class="code-toolbar__code">{{ localInstallCommand }}</pre>
                    <t-button
                      class="code-toolbar__copy"
                      size="small"
                      variant="text"
                      shape="square"
                      :title="$t('integrations.claw.copy')"
                      @click="copyInstallCommand"
                    >
                      <t-icon name="file-copy" size="16px" />
                    </t-button>
                  </div>
                </div>
              </div>
            </li>
          </ol>
        </section>
      </div>
    </template>

    <template #footer>
      <div class="landing-footer-bar" role="note">
        <p class="landing-footer-bar__note">{{ $t('integrations.claw.ecosystemNote') }}</p>
        <span class="landing-meta">{{ $t('integrations.claw.hubMeta') }}</span>
      </div>
    </template>
  </IntegrationLandingLayout>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import { copyWithToast } from '@/utils/clipboard'
import { useRouter } from 'vue-router'
import { downloadWeKnoraSkill } from '@/api/skill'
import { CLAWHUB_SKILL_URL } from '@/config/integrations'
import { useApiBaseUrlDisplay } from '@/composables/useApiBaseUrlDisplay'
import { useUIStore } from '@/stores/ui'
import IntegrationLandingLayout from './IntegrationLandingLayout.vue'
import IntegrationExternalCta from './IntegrationExternalCta.vue'

const { t } = useI18n()
const router = useRouter()
const uiStore = useUIStore()
const { apiBaseUrlDisplay } = useApiBaseUrlDisplay()
const downloading = ref(false)

const capabilityKeys = ['upload', 'url', 'manual', 'search', 'browse'] as const
const stepKeys = ['api', 'download', 'env', 'verify'] as const

const capabilityIcons: Record<(typeof capabilityKeys)[number], string> = {
  upload: 'upload',
  url: 'link',
  manual: 'edit',
  search: 'search',
  browse: 'view-list',
}

const localInstallCommand = 'unzip weknora-skill.zip\nopenclaw skills install ./weknora'

const envExample = 'export WEKNORA_API_KEY="sk-your-api-key"'

const openClawHub = () => {
  window.open(CLAWHUB_SKILL_URL, '_blank', 'noopener,noreferrer')
}

const downloadSkill = async () => {
  if (!apiBaseUrlDisplay.value || downloading.value) return

  downloading.value = true
  try {
    // BASE_URL can be a reverse-proxy subpath such as /app/weknora.
    // The downloaded skill runs outside this page, so embed an absolute URL.
    const baseUrl = new URL(apiBaseUrlDisplay.value, window.location.origin).toString().replace(/\/$/, '')
    const blob = await downloadWeKnoraSkill(baseUrl)
    const objectUrl = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = objectUrl
    anchor.download = 'weknora-skill.zip'
    document.body.appendChild(anchor)
    anchor.click()
    document.body.removeChild(anchor)
    setTimeout(() => URL.revokeObjectURL(objectUrl), 1000)
    MessagePlugin.success(t('integrations.claw.downloadSuccess'))
  } catch (error) {
    console.error('[ClawSkillLanding] skill download failed:', error)
    MessagePlugin.error(t('integrations.claw.downloadFailed'))
  } finally {
    downloading.value = false
  }
}

const openApiSettings = () => {
  router.push({ path: '/platform/settings', query: { section: 'integration-api' } })
  uiStore.openSettings('integration-api')
}

const copyEnvExample = () => copyWithToast(envExample, 'integrations.claw.copyEnvSuccess')
const copyInstallCommand = () => copyWithToast(localInstallCommand, 'integrations.claw.copyCmdSuccess')
</script>
