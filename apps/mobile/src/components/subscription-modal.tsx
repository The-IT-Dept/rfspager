/**
 * SubscriptionModal - Popup subscription upsell modal
 * 
 * Triggered from ad clicks to provide seamless upgrade experience
 * without complex navigation through multiple screens.
 */

import React, { useState, useEffect } from "react"
import {
  View,
  StyleSheet,
  TouchableOpacity,
  Alert,
  ActivityIndicator,
  ScrollView,
  Text,
  Platform,
  Modal,
} from "react-native"
import { FontAwesome5, AntDesign } from "@expo/vector-icons"
import {
  getProducts,
  purchaseSubscription,
  restorePurchases,
  setupPurchaseListeners,
  type PurchaseProduct,
  type ValidationResult,
} from "../services/purchase-service"

import { useValidSubscription } from "../stores"
import { colors, useAppTheme } from "../theme"
import { PREMIUM_FEATURES } from "../config/premium-features"
import { trackAdConversion } from "../utils/ad-analytics"

interface SubscriptionModalProps {
  visible: boolean
  onDismiss: () => void
  source?: "native_ad" | "interstitial_ad" | "banner_ad"
}

export function SubscriptionModal({ visible, onDismiss, source = "native_ad" }: SubscriptionModalProps) {
  const validSubscription = useValidSubscription()
  const theme = useAppTheme()
  const [products, setProducts] = useState<PurchaseProduct[]>([])
  const [isPurchasing, setIsPurchasing] = useState(false)
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    if (visible && !validSubscription) {
      loadProducts()
    }
  }, [visible, validSubscription])

  useEffect(() => {
    const cleanup = setupPurchaseListeners(
      (result: ValidationResult) => {
        if (result.valid) {
          trackAdConversion(source === "native_ad" ? "native" : "interstitial", "modal_upsell", 4.99)
          onDismiss()
        }
      },
      (error: any) => {
        Alert.alert("Purchase Error", error?.message || "An error occurred")
      },
    )
    return cleanup
  }, [])

  const loadProducts = async () => {
    try {
      setIsLoading(true)
      const prods = await getProducts()
      setProducts(prods)
    } catch (error) {
      console.error("Failed to load products:", error)
      Alert.alert("Error", "Failed to load subscription options. Please try again.")
    } finally {
      setIsLoading(false)
    }
  }

  const handlePurchase = async (product: PurchaseProduct) => {
    setIsPurchasing(true)
    try {
      await purchaseSubscription(product.productId)
    } catch (error: any) {
      if (error?.code !== "E_USER_CANCELLED" && error?.code !== "user-cancelled") {
        Alert.alert("Error purchasing", error?.message || "An error occurred")
      }
    } finally {
      setIsPurchasing(false)
    }
  }

  const handleRestore = async () => {
    try {
      const result = await restorePurchases()
      if (result?.valid) {
        onDismiss()
      } else {
        Alert.alert("No Purchases", "No active subscriptions found to restore.")
      }
    } catch (error) {
      console.error("Restore failed:", error)
    }
  }

  const getIconName = (iconName: string) => {
    switch (iconName) {
      case "location-outline":
        return "map-marker-alt"
      case "filter-outline":
        return "filter"
      case "help-circle-outline":
        return "eye-slash"
      case "headset-outline":
        return "headset"
      case "notifications-outline":
        return "bell"
      case "download-outline":
        return "download"
      default:
        return "star"
    }
  }

  if (validSubscription) {
    return null
  }

  return (
    <Modal
      visible={visible}
      onRequestClose={onDismiss}
      presentationStyle="pageSheet"
      animationType="slide"
    >
      <View style={[styles.sheetContainer, { backgroundColor: theme.colors.backgroundTertiary }]}>
        <View style={[styles.modal, { backgroundColor: theme.colors.backgroundTertiary }]}>
          {/* Header */}
          <View style={[styles.header, { backgroundColor: theme.colors.cardBackground }]}>
            <TouchableOpacity onPress={onDismiss} style={styles.closeButton}>
              <AntDesign name="close" size={24} color={theme.colors.textSecondary} />
            </TouchableOpacity>
            
            <View style={styles.headerContent}>
              <View style={[styles.iconContainer, { backgroundColor: theme.colors.primary }]}>
                <AntDesign name="gift" size={24} color={colors.white} />
              </View>
              <Text style={[styles.title, { color: theme.colors.textPrimary }]}>Upgrade to Premium</Text>
              <Text style={[styles.subtitle, { color: theme.colors.textSecondary }]}>
                Remove ads and unlock all features
              </Text>
            </View>
          </View>

          {/* Content */}
          <ScrollView style={styles.content} showsVerticalScrollIndicator={false}>
            {isLoading ? (
              <View style={styles.loadingContainer}>
                <ActivityIndicator size="large" color={theme.colors.primary} />
                <Text style={[styles.loadingText, { color: theme.colors.textSecondary }]}>
                  Loading subscription options...
                </Text>
              </View>
            ) : (
              <>
                {/* Features List */}
                <View style={styles.featuresSection}>
                  <Text style={[styles.sectionTitle, { color: theme.colors.textPrimary }]}>Pro Features</Text>
                  
                  {/* Ad-Free Experience - special case not in config */}
                  <View style={styles.featureRow}>
                    <View style={[styles.featureIcon, { backgroundColor: "transparent" }]}>
                      <FontAwesome5 name="ad" size={16} color={theme.colors.primary} />
                    </View>
                    <View style={styles.featureContent}>
                      <Text style={[styles.featureTitle, { color: theme.colors.textPrimary }]}>
                        Ad-Free Experience
                      </Text>
                      <Text style={[styles.featureDescription, { color: theme.colors.textSecondary }]}>
                        Remove all advertisements for a clean, professional interface
                      </Text>
                    </View>
                  </View>

                  {PREMIUM_FEATURES.map((feature, index) => {
                    const isLastItem = index === PREMIUM_FEATURES.length - 1

                    return (
                      <View key={feature.id} style={isLastItem ? styles.featureRowLast : styles.featureRow}>
                        <View style={[styles.featureIcon, { backgroundColor: "transparent" }]}>
                          <FontAwesome5
                            name={getIconName(feature.icon)}
                            size={16}
                            color={theme.colors.primary}
                          />
                        </View>
                        <View style={styles.featureContent}>
                          <Text style={[styles.featureTitle, { color: theme.colors.textPrimary }]}>
                            {feature.title}
                          </Text>
                          <Text style={[styles.featureDescription, { color: theme.colors.textSecondary }]}>
                            {feature.description}
                          </Text>
                        </View>
                      </View>
                    )
                  })}
                </View>

                {/* Subscription Plans */}
                {products.length > 0 && (
                  <View style={styles.plansContainer}>
                    <Text style={[styles.plansTitle, { color: theme.colors.textPrimary }]}>
                      Choose Your Plan
                    </Text>

                    {products.map((product) => {
                      const isYearly = product.productId.includes("yearly")
                      return (
                        <TouchableOpacity
                          key={product.productId}
                          style={[
                            styles.planRow,
                            { borderColor: theme.colors.border },
                            isYearly && styles.planRowRecommended,
                          ]}
                          onPress={() => handlePurchase(product)}
                          disabled={isPurchasing}
                        >
                          <View style={styles.planContent}>
                            <Text style={[styles.planTitle, { color: theme.colors.textPrimary }]}>
                              {product.title}
                            </Text>
                            <Text style={[styles.planDescription, { color: theme.colors.textSecondary }]}>
                              {isYearly
                                ? "Best value - Save 40% with annual billing"
                                : "Flexible monthly subscription"}
                            </Text>
                          </View>
                          <View style={styles.planPrice}>
                            <Text style={[styles.priceText, { color: theme.colors.primary }]}>
                              {product.localizedPrice}
                            </Text>
                            {isYearly && (
                              <Text style={[styles.savingsText, { color: theme.colors.primary }]}>
                                RECOMMENDED
                              </Text>
                            )}
                          </View>
                        </TouchableOpacity>
                      )
                    })}

                    {Platform.OS === "ios" && (
                      <TouchableOpacity
                        style={styles.restoreButton}
                        onPress={handleRestore}
                      >
                        <Text style={[styles.restoreText, { color: theme.colors.primary }]}>
                          Restore Purchases
                        </Text>
                      </TouchableOpacity>
                    )}
                  </View>
                )}

                {/* Terms */}
                <Text style={[styles.termsText, { color: theme.colors.textTertiary }]}>
                  RFSPager Pro subscriptions are billed through your{" "}
                  {Platform.OS === "ios" ? "App Store" : "Google Play"} account. Subscriptions
                  automatically renew unless cancelled at least 24 hours before the current period ends.
                  You can manage your subscription in your account settings.
                </Text>
              </>
            )}
          </ScrollView>

          {isPurchasing && (
            <View style={styles.purchasingOverlay}>
              <ActivityIndicator size="large" color={theme.colors.primary} />
            </View>
          )}
        </View>
      </View>
    </Modal>
  )
}

const styles = StyleSheet.create({
  sheetContainer: {
    flex: 1,
  },

  modal: {
    flex: 1,
    overflow: "hidden",
  },
  
  header: {
    position: "relative",
    paddingTop: 20,
    paddingBottom: 24,
    paddingHorizontal: 24,
  },
  
  closeButton: {
    position: "absolute",
    top: 16,
    right: 16,
    zIndex: 1,
    padding: 4,
  },
  
  headerContent: {
    alignItems: "center",
  },

  iconContainer: {
    width: 48,
    height: 48,
    borderRadius: 24,
    alignItems: "center",
    justifyContent: "center",
    marginBottom: 16,
  },
  
  title: {
    fontSize: 24,
    fontWeight: "700",
    textAlign: "center",
    marginBottom: 8,
  },
  
  subtitle: {
    fontSize: 16,
    textAlign: "center",
  },
  
  content: {
    paddingHorizontal: 16,
    paddingBottom: 24,
  },
  
  loadingContainer: {
    alignItems: "center",
    paddingVertical: 40,
  },
  
  loadingText: {
    marginTop: 16,
    fontSize: 16,
  },

  featuresSection: {
    marginTop: 16,
    marginBottom: 16,
  },

  sectionTitle: {
    fontSize: 15,
    fontWeight: "600",
    marginBottom: 12,
  },
  
  featureRow: {
    flexDirection: "row",
    alignItems: "flex-start",
    paddingVertical: 12,
    borderBottomWidth: 0.5,
    borderBottomColor: colors.borderSecondary,
  },

  featureRowLast: {
    flexDirection: "row",
    alignItems: "flex-start",
    paddingVertical: 12,
  },
  
  featureIcon: {
    width: 24,
    height: 24,
    justifyContent: "center",
    alignItems: "center",
    marginRight: 12,
    marginTop: 2,
  },
  
  featureContent: {
    flex: 1,
  },

  featureTitle: {
    fontSize: 16,
    fontWeight: "600",
    marginBottom: 4,
  },

  featureDescription: {
    fontSize: 14,
    lineHeight: 20,
  },

  plansContainer: {
    marginBottom: 16,
  },

  plansTitle: {
    fontSize: 15,
    fontWeight: "600",
    marginBottom: 12,
  },

  planRow: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 16,
    paddingVertical: 16,
    borderBottomWidth: 0.5,
    borderBottomColor: colors.borderSecondary,
    backgroundColor: colors.cardBackground,
    marginBottom: 8,
    borderRadius: 8,
  },

  planRowRecommended: {
    backgroundColor: colors.primary + "10",
  },

  planContent: {
    flex: 1,
  },

  planTitle: {
    fontSize: 17,
    fontWeight: "600",
    marginBottom: 4,
  },

  planDescription: {
    fontSize: 14,
  },

  planPrice: {
    alignItems: "flex-end",
  },

  priceText: {
    fontSize: 17,
    fontWeight: "600",
  },

  savingsText: {
    fontSize: 11,
    fontWeight: "600",
    backgroundColor: colors.primary + "15",
    paddingHorizontal: 6,
    paddingVertical: 2,
    borderRadius: 4,
    overflow: "hidden",
    marginTop: 2,
  },

  restoreButton: {
    paddingHorizontal: 16,
    paddingVertical: 16,
    alignItems: "center",
  },
  
  restoreText: {
    fontSize: 16,
    fontWeight: "400",
  },
  
  termsText: {
    fontSize: 13,
    textAlign: "justify",
    lineHeight: 18,
    marginTop: 12,
  },

  purchasingOverlay: {
    ...StyleSheet.absoluteFill,
    backgroundColor: "rgba(0, 0, 0, 0.75)",
    alignItems: "center",
    justifyContent: "center",
  },
})
