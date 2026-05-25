/**
 * useSubscriptionModal - Hook for managing subscription modal
 * 
 * Provides a simple interface for showing subscription modals
 * from anywhere in the app, particularly from ads.
 */

import React, { useState, useCallback } from "react"
import { SubscriptionModal } from "../components/subscription-modal"

export const useSubscriptionModal = () => {
  const [isVisible, setIsVisible] = useState(false)
  const [source, setSource] = useState<"native_ad" | "interstitial_ad" | "banner_ad">("native_ad")

  const showModal = useCallback((adSource: "native_ad" | "interstitial_ad" | "banner_ad" = "native_ad") => {
    setSource(adSource)
    setIsVisible(true)
  }, [])

  const hideModal = useCallback(() => {
    setIsVisible(false)
  }, [])

  const ModalComponent = useCallback(() => (
    <SubscriptionModal 
      visible={isVisible}
      onDismiss={hideModal}
      source={source}
    />
  ), [isVisible, hideModal, source])

  return {
    showModal,
    hideModal,
    ModalComponent,
    isVisible
  }
}