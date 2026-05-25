/**
 * Premium Features Configuration
 * Shared between onboarding and subscription screens
 */

export interface PremiumFeature {
  id: string
  title: string
  description: string
  icon: string
  category: "alerts" | "customization" | "data"
}

export const PREMIUM_FEATURES: PremiumFeature[] = [
  {
    id: "alert-zones",
    title: "Alert Zones",
    description:
      "Create custom geographic zones and receive notifications when incidents occur in your areas of interest",
    icon: "location-outline",
    category: "alerts",
  },
  {
    id: "capcode-filtering",
    title: "Capcode Filtering",
    description:
      "Filter messages to show only selected capcodes, reducing noise and focusing on relevant information",
    icon: "filter-outline",
    category: "customization",
  },
  {
    id: "unknown-capcodes",
    title: "Unknown Capcodes",
    description:
      "View messages from unidentified capcodes to stay informed about all emergency communications",
    icon: "help-circle-outline",
    category: "data",
  },
  {
    id: "priority-support",
    title: "Priority Support",
    description: "Get direct access to our support team for faster resolution of technical issues",
    icon: "headset-outline",
    category: "customization",
  },
  //{
  //  id: 'advanced-notifications',
  //  title: 'Advanced Notifications',
  //  description: 'Customize notification sounds, vibration patterns, and quiet hours for different types of incidents',
  //  icon: 'notifications-outline',
  //  category: 'alerts'
  //},
  //{
  //  id: 'data-export',
  //  title: 'Data Export',
  //  description: 'Export incident data and message history for analysis, reporting, or record keeping',
  //  icon: 'download-outline',
  //  category: 'data'
  //}
]

export const PREMIUM_BENEFITS = [
  "Support independent development",
  "Help keep the service running",
  "Access to premium features",
  "Priority customer support",
  "No advertisements",
]

export const getPremiumFeaturesByCategory = (category: PremiumFeature["category"]) =>
  PREMIUM_FEATURES.filter((feature) => feature.category === category)

export const getAllPremiumFeatures = () => PREMIUM_FEATURES
