import { useState } from 'react'
import { Modal, Form, Input, App } from 'antd'
import { useTranslation } from 'react-i18next'
import { api } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { formatApiError } from '@/api/errors'

interface Props {
  open: boolean
  onClose: () => void
  forced?: boolean
}

export default function ChangePasswordModal({ open, onClose, forced }: Props) {
  const { t } = useTranslation()
  const { message } = App.useApp()
  const setUser = useAuthStore((s) => s.setUser)
  const user = useAuthStore((s) => s.user)
  const [form] = Form.useForm()
  const [loading, setLoading] = useState(false)
  // SSO-auto-created accounts have no password the user knows. The
  // "set password" path matches the forced-rotation UI: no current-
  // password field, friendlier title. Backend mirrors this — it skips
  // the old-password check when the user's hasPassword flag is false.
  // Use the truthy check (not strict ===) so undefined (e.g. /auth/me
  // hadn't loaded yet, or a cached store from before the field
  // existed) ALSO falls into setMode rather than dead-locking the
  // user with an old-password prompt for a password they never had.
  const setMode = !forced && !user?.hasPassword

  const onSubmit = async () => {
    const v = await form.validateFields()
    setLoading(true)
    try {
      await api.post('/auth/me/password', v)
      if (user) setUser({ ...user, mustChangePassword: false, hasPassword: true })
      message.success(t('common.success'))
      form.resetFields()
      onClose()
    } catch (e: any) {
      message.error(formatApiError(e, 'common.error'))
    } finally { setLoading(false) }
  }

  const showOldField = !forced && !setMode
  const title = forced
    ? t('user.firstLoginMust')
    : setMode
      ? t('user.setPassword')
      : t('user.changePassword')

  return (
    <Modal
      open={open}
      onCancel={forced ? undefined : onClose}
      onOk={onSubmit}
      confirmLoading={loading}
      closable={!forced}
      maskClosable={!forced}
      keyboard={!forced}
      title={title}
      destroyOnClose
    >
      <Form form={form} layout="vertical">
        {showOldField && (
          <Form.Item name="oldPassword" label={t('user.oldPassword')} rules={[{ required: true }]}>
            <Input.Password autoComplete="current-password" />
          </Form.Item>
        )}
        <Form.Item name="newPassword" label={t('user.newPasswordField')} rules={[{ required: true, min: 4 }]}>
          <Input.Password autoComplete="new-password" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
