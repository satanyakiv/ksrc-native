@kotlinx/cinterop/ExternalObjCClass
public open class platform/UserNotifications/UNNotificationMeta : platform/darwin/NSObjectMeta, platform/Foundation/NSCopyingProtocolMeta, platform/Foundation/NSSecureCodingProtocolMeta {

  protected /* secondary */ constructor()

  @kotlinx/cinterop/ObjCMethod(selector = "new", encoding = "@16@0:8", isStret = false)
  public open external fun new(): platform/UserNotifications/UNNotification?

  @kotlinx/cinterop/ObjCMethod(selector = "allocWithZone:", encoding = "@24@0:8^{_NSZone=}16", isStret = false)
  @kotlin/Deprecated(message = "Use constructor or factory method instead", replaceWith = kotlin/ReplaceWith(imports = [], expression = ""), level = kotlin/DeprecationLevel.WARNING)
  public open external fun allocWithZone(zone: kotlinx/cinterop/CPointer<cnames/structs/_NSZone>?): platform/UserNotifications/UNNotification?

  @kotlinx/cinterop/ObjCMethod(selector = "alloc", encoding = "@16@0:8", isStret = false)
  @kotlin/Deprecated(message = "Use constructor or factory method instead", replaceWith = kotlin/ReplaceWith(imports = [], expression = ""), level = kotlin/DeprecationLevel.WARNING)
  public open external fun alloc(): platform/UserNotifications/UNNotification?

  @kotlinx/cinterop/ObjCMethod(selector = "supportsSecureCoding", encoding = "B16@0:8", isStret = false)
  public open external fun supportsSecureCoding(): kotlin/Boolean
}
