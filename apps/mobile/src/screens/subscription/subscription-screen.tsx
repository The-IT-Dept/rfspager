/**
 * subscription screen
 * @format
 */
import React, { useEffect, FC, useState } from "react"
import { View, Alert, ScrollView, ActivityIndicator, Linking, Platform, Text } from "react-native"
import { FontAwesome5, AntDesign } from "@expo/vector-icons"
import {
  getProducts,
  purchaseSubscription,
  restorePurchases,
  setupPurchaseListeners,
  type PurchaseProduct,
  type ValidationResult,
} from "../../services/purchase-service"
import { TouchableOpacity } from "react-native-gesture-handler"
import { useRouter } from "expo-router"
import { useAuthStore, useValidSubscription, useProductName } from "../../stores"
import { makeStyles } from "./styles"
import { useAppTheme } from "../../theme"
import { PREMIUM_FEATURES } from "../../config/premium-features"

export const SubscriptionScreen: FC = () => {
    const router = useRouter()
    const validSubscription = useValidSubscription()
    const productName = useProductName()
    const subscriptionExpiryDate = useAuthStore((s) => s.user?.subscription_expiry?.toString() || "")
    const refreshUser = useAuthStore((s) => s.refreshUser)
    const theme = useAppTheme()
    const styles = makeStyles(theme)

    const [products, setProducts] = useState<PurchaseProduct[]>([])
    const [isPurchasing, setIsPurchasing] = useState<boolean>(false)

    // Ensure fresh user data when screen loads
    useEffect(() => {
      refreshUser()
    }, [])


    useEffect(() => {
      getProducts().then(setProducts).catch(console.error)

      const cleanup = setupPurchaseListeners(
        (result: ValidationResult) => {
          if (result.valid) {
            refreshUser()
            router.back()
          }
        },
        (error: any) => {
          Alert.alert("Purchase Error", error?.message || "An error occurred")
        },
      )

      return cleanup
    }, [])

    return (
      <ScrollView
        style={styles.container}
        showsVerticalScrollIndicator={false}
        contentInsetAdjustmentBehavior="automatic"
      >
        {/* Current Subscription Status */}
        <View style={styles.mainContainer}>
          <View style={styles.headerRow}>
            <View style={[styles.iconContainer, { backgroundColor: theme.colors.primary }]}>
              <AntDesign name="gift" size={10} color="white" />
            </View>
            <View style={styles.headerContent}>
              <Text style={styles.statusTitle}>
                {validSubscription ? "RFSPager Pro" : "Free Plan"}
              </Text>
              <Text style={styles.statusSubtitle}>
                {validSubscription ? productName : "Limited features"}
              </Text>
            </View>
            {validSubscription && (
              <Text style={styles.expiryDate}>Renews {subscriptionExpiryDate.split("T")[0]}</Text>
            )}
          </View>

          {validSubscription ? (
            <View style={styles.dataRow}>
              <Text style={styles.dataLabel}>Status</Text>
              <Text style={styles.dataValue}>Active subscription to {productName}</Text>
            </View>
          ) : (
            <View style={styles.dataRow}>
              <Text style={styles.dataLabel}>Status</Text>
              <Text style={styles.dataValue}>Not subscribed - upgrade to unlock all features</Text>
            </View>
          )}
        </View>

        {/* Pro Features */}
        <View style={styles.sectionContainer}>
          <View style={styles.sectionHeader}>
            <Text style={styles.sectionTitle}>Pro Features</Text>
          </View>
          {/* Ad-Free Experience - special case not in config */}
          <View style={styles.featureRow}>
            <View style={styles.featureIcon}>
              <FontAwesome5 name="ad" size={16} color={theme.colors.interactive} />
            </View>
            <View style={styles.featureContent}>
              <Text style={styles.featureTitle}>Ad-Free Experience</Text>
              <Text style={styles.featureDescription}>
                Remove all advertisements for a clean, professional interface
              </Text>
            </View>
          </View>

          {PREMIUM_FEATURES.map((feature, index) => {
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

            const isLastItem = index === PREMIUM_FEATURES.length - 1

            return (
              <View key={feature.id} style={isLastItem ? styles.featureRowLast : styles.featureRow}>
                <View style={styles.featureIcon}>
                  <FontAwesome5
                    name={getIconName(feature.icon)}
                    size={16}
                    color={theme.colors.interactive}
                  />
                </View>
                <View style={styles.featureContent}>
                  <Text style={styles.featureTitle}>{feature.title}</Text>
                  <Text style={styles.featureDescription}>{feature.description}</Text>
                </View>
              </View>
            )
          })}
        </View>

        {/* Subscription Plans */}
        {products.length > 0 && (
          <View style={styles.plansContainer}>
            <View style={styles.plansHeader}>
              <Text style={styles.plansTitle}>Choose Your Plan</Text>
            </View>
            {products.map((product) => {
              const isYearly = product.productId.includes("yearly")
              return (
                <TouchableOpacity
                  key={product.productId}
                  style={[
                    styles.planRow,
                    isYearly && styles.planRowRecommended,
                  ]}
                  onPress={async () => {
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
                  }}
                >
                  <View style={styles.planContent}>
                    <Text style={styles.planTitle}>{product.title}</Text>
                    <Text style={styles.planDescription}>
                      {isYearly
                        ? "Best value - Save 40% with annual billing"
                        : "Flexible monthly subscription"}
                    </Text>
                  </View>
                  <View style={styles.planPrice}>
                    <Text style={styles.priceText}>{product.localizedPrice}</Text>
                    {isYearly && (
                      <Text style={styles.savingsText}>RECOMMENDED</Text>
                    )}
                  </View>
                </TouchableOpacity>
              )
            })}

            {Platform.OS === "ios" && (
              <TouchableOpacity
                style={styles.restoreButton}
                onPress={async () => {
                  try {
                    const result = await restorePurchases()
                    if (result?.valid) {
                      refreshUser()
                      router.back()
                    } else {
                      Alert.alert("No Purchases", "No active subscriptions found to restore.")
                    }
                  } catch (error) {
                    console.error("Restore failed:", error)
                  }
                }}
              >
                <Text style={styles.restoreText}>Restore Purchases</Text>
              </TouchableOpacity>
            )}
          </View>
        )}

        {/* Terms and Privacy */}
        <View style={styles.legalContainer}>
          <Text style={styles.legalText}>
            RFSPager Pro subscriptions are billed through your{" "}
            {Platform.OS === "ios" ? "App Store" : "Google Play"} account. Subscriptions
            automatically renew unless cancelled at least 24 hours before the current period ends.
            You can manage your subscription in your account settings.
          </Text>

          <View style={styles.legalLinksRow}>
            <TouchableOpacity
              onPress={() => Linking.openURL("https://rfspager.app/terms-of-service")}
            >
              <Text style={styles.legalLink}>Terms of Service</Text>
            </TouchableOpacity>
            <Text style={styles.legalSeparator}>•</Text>
            <TouchableOpacity
              onPress={() => Linking.openURL("https://rfspager.app/privacy-policy")}
            >
              <Text style={styles.legalLink}>Privacy Policy</Text>
            </TouchableOpacity>
          </View>
        </View>

        {isPurchasing && (
          <View style={styles.portalView} pointerEvents="auto">
            <ActivityIndicator size={"large"} color={theme.colors.primary} />
          </View>
        )}
      </ScrollView>
    )
  }
