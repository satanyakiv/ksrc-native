@kotlinx/cinterop/ExternalObjCClass(protocolGetter = "kniprot_platform_UserNotifications_UNUserNotificationCenterDelegate_0")
public abstract interface platform/UserNotifications/UNUserNotificationCenterDelegateProtocol : platform/darwin/NSObjectProtocol {

  @kotlinx/cinterop/ObjCMethod(selector = "userNotificationCenter:willPresentNotification:withCompletionHandler:", encoding = "v40@0:8@16@24@?32", isStret = false)
  public open fun userNotificationCenter(center: platform/UserNotifications/UNUserNotificationCenter, willPresentNotification: platform/UserNotifications/UNNotification, withCompletionHandler: kotlin/Function1<kotlin/ULong /* = platform/UserNotifications/UNNotificationPresentationOptions^ */, kotlin/Unit>): kotlin/Unit

  @kotlinx/cinterop/ObjCMethod(selector = "userNotificationCenter:didReceiveNotificationResponse:withCompletionHandler:", encoding = "v40@0:8@16@24@?32", isStret = false)
  public open fun userNotificationCenter(center: platform/UserNotifications/UNUserNotificationCenter, didReceiveNotificationResponse: platform/UserNotifications/UNNotificationResponse, withCompletionHandler: kotlin/Function0<kotlin/Unit>): kotlin/Unit

  @kotlinx/cinterop/ObjCMethod(selector = "userNotificationCenter:openSettingsForNotification:", encoding = "v32@0:8@16@24", isStret = false)
  public open fun userNotificationCenter(center: platform/UserNotifications/UNUserNotificationCenter, openSettingsForNotification: platform/UserNotifications/UNNotification?): kotlin/Unit
}
