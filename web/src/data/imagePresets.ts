// Curated catalogue of common Docker images shown in the "Pull image" modal.
// Strings are i18n keys (resolved with t() in the page).

export interface ImagePreset {
  ref: string         // full image reference passed to docker pull
  labelKey: string    // i18n key for short display label
  descKey?: string    // i18n key for one-line description
  size?: string       // approximate compressed download size (informational)
}

export interface PresetGroup {
  id: string
  titleKey: string    // i18n key for group title
  hintKey?: string    // i18n key for group hint
  items: ImagePreset[]
}

export const PRESET_GROUPS: PresetGroup[] = [
  {
    id: 'java',
    titleKey: 'images.presets.groups.java.title',
    hintKey:  'images.presets.groups.java.hint',
    items: [
      { ref: 'eclipse-temurin:8-jre',  labelKey: 'images.presets.jdk8.label',  descKey: 'images.presets.jdk8.desc',  size: '~210 MB' },
      { ref: 'eclipse-temurin:11-jre', labelKey: 'images.presets.jdk11.label', descKey: 'images.presets.jdk11.desc', size: '~230 MB' },
      { ref: 'eclipse-temurin:17-jre', labelKey: 'images.presets.jdk17.label', descKey: 'images.presets.jdk17.desc', size: '~250 MB' },
      { ref: 'eclipse-temurin:21-jre', labelKey: 'images.presets.jdk21.label', descKey: 'images.presets.jdk21.desc', size: '~250 MB' },
      { ref: 'eclipse-temurin:25-jre', labelKey: 'images.presets.jdk25.label', descKey: 'images.presets.jdk25.desc', size: '~250 MB' },
    ],
  },
  {
    id: 'bedrock',
    titleKey: 'images.presets.groups.bedrock.title',
    hintKey:  'images.presets.groups.bedrock.hint',
    items: [
      { ref: 'ubuntu:22.04', labelKey: 'images.presets.bedrock.label', descKey: 'images.presets.bedrock.desc', size: '~30 MB' },
    ],
  },
  {
    id: 'runtime',
    titleKey: 'images.presets.groups.runtime.title',
    items: [
      { ref: 'alpine:latest',         labelKey: 'images.presets.alpine.label',  descKey: 'images.presets.alpine.desc',  size: '~5 MB' },
      { ref: 'debian:bookworm-slim',  labelKey: 'images.presets.debian.label',  descKey: 'images.presets.debian.desc',  size: '~30 MB' },
      { ref: 'ubuntu:24.04',          labelKey: 'images.presets.ubuntu.label',  descKey: 'images.presets.ubuntu.desc',  size: '~30 MB' },
    ],
  },
  {
    id: 'lang',
    titleKey: 'images.presets.groups.lang.title',
    items: [
      { ref: 'python:3-slim',       labelKey: 'images.presets.python.label', descKey: 'images.presets.python.desc', size: '~50 MB' },
      { ref: 'node:20-alpine',      labelKey: 'images.presets.node.label',   descKey: 'images.presets.node.desc',   size: '~50 MB' },
      { ref: 'golang:1.22-alpine',  labelKey: 'images.presets.go.label',     descKey: 'images.presets.go.desc',     size: '~150 MB' },
    ],
  },
  {
    id: 'web',
    titleKey: 'images.presets.groups.web.title',
    items: [
      { ref: 'nginx:alpine', labelKey: 'images.presets.nginx.label', descKey: 'images.presets.nginx.desc', size: '~50 MB' },
      { ref: 'caddy:alpine', labelKey: 'images.presets.caddy.label', descKey: 'images.presets.caddy.desc', size: '~50 MB' },
    ],
  },
  {
    id: 'db',
    titleKey: 'images.presets.groups.db.title',
    items: [
      { ref: 'mysql:8',            labelKey: 'images.presets.mysql.label',    descKey: 'images.presets.mysql.desc',    size: '~600 MB' },
      { ref: 'mariadb:11',         labelKey: 'images.presets.mariadb.label',  descKey: 'images.presets.mariadb.desc',  size: '~150 MB' },
      { ref: 'postgres:16-alpine', labelKey: 'images.presets.postgres.label', descKey: 'images.presets.postgres.desc', size: '~80 MB' },
      { ref: 'redis:7-alpine',     labelKey: 'images.presets.redis.label',    descKey: 'images.presets.redis.desc',    size: '~40 MB' },
    ],
  },
]
