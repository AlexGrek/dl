interface Props {
  title: string;
  message: string;
  confirmLabel?: string;
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmModal({
  title,
  message,
  confirmLabel = 'delete',
  onConfirm,
  onCancel,
}: Props) {
  function handleOverlayClick(e: MouseEvent) {
    if ((e.target as HTMLElement).classList.contains('modal-overlay')) onCancel();
  }

  return (
    <div class="modal-overlay" onClick={handleOverlayClick} data-modal="confirm">
      <div class="modal" role="dialog" aria-modal="true" aria-labelledby="confirm-modal-title">
        <p class="modal__title" id="confirm-modal-title">
          {title}
        </p>
        <p class="modal__body">{message}</p>
        <div class="modal__actions">
          <button type="button" class="btn btn--muted" id="btn-confirm-modal-cancel" onClick={onCancel}>
            cancel
          </button>
          <button type="button" class="btn btn--danger" id="btn-confirm-modal-confirm" onClick={onConfirm}>
            {confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}
