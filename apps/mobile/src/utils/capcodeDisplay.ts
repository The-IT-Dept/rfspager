export const capcodeDisplay = (message: any) => {
  const agency = message?.agency_name || message?.capcode?.agency || "Address"
  const alias = message?.capcode_alias || message?.capcode?.alias || message?.address || "Unknown"
  return `${agency} - ${alias}`
}
