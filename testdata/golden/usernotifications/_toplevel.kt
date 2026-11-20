public typealias UNNotificationSoundNameVar = kotlinx/cinterop/ObjCObjectVar<platform/UserNotifications/UNNotificationSoundName^> /* = kotlinx/cinterop/ObjCObjectVar<kotlin/String? /* = platform/UserNotifications/UNNotificationSoundName^ */> */

public typealias UNNotificationSoundName = kotlin/String? /* = kotlin/String? */

public typealias UNNotificationPresentationOptions = platform/darwin/NSUInteger^ /* = kotlin/ULong /* = platform/darwin/NSUInteger^ */ */

@kotlinx/cinterop/ObjCMethod(selector = "localizedUserNotificationStringForKey:arguments:", encoding = "@32@0:8@16@24", isStret = false)
public final external fun platform/Foundation/NSStringMeta.localizedUserNotificationStringForKey(key: kotlin/String, arguments: kotlin/collections/List<*>?): kotlin/String

public final val UNAuthorizationOptionNone: kotlin/ULong /* = platform/UserNotifications/UNAuthorizationOptions^ */
  @kotlinx/cinterop/internal/CCall(id = "knifunptr_platform_UserNotifications_UNAuthorizationOptionNone_getter_0")
  public final /* non-default */ external get

public final val UNErrorDomain: kotlin/String?
  @kotlinx/cinterop/internal/CCall(id = "knifunptr_platform_UserNotifications_UNErrorDomain_getter_0")
  public final /* non-default */ external get

public final const val UNNotificationPresentationOptionBanner: kotlin/ULong /* = platform/UserNotifications/UNNotificationPresentationOptions^ */ /* = 16uL */
  public final get
