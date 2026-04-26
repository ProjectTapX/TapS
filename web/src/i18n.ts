import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'

import { zh } from './i18n/zh'
import { en } from './i18n/en'
import { ja } from './i18n/ja'

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      zh: { translation: zh },
      en: { translation: en },
      ja: { translation: ja },
    },
    fallbackLng: 'zh',
    // escapeValue:true makes i18next HTML-escape any interpolated
    // value. React already auto-escapes string children, so this is
    // defense-in-depth against XSS via backend-supplied params.
    interpolation: { escapeValue: true },
    detection: { order: ['localStorage', 'navigator'], caches: ['localStorage'] },
  })

export default i18n
