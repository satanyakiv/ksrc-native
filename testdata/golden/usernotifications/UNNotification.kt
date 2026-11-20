@kotlinx/cinterop/ExternalObjCClass
public open class platform/UserNotifications/UNNotification : platform/darwin/NSObject, platform/Foundation/NSCopyingProtocol, platform/Foundation/NSSecureCodingProtocol {

  @kotlinx/cinterop/ObjCUnavailable
  @kotlin/Deprecated(message = "This Objective-C declaration is unavailable", replaceWith = kotlin/ReplaceWith(imports = [], expression = ""), level = kotlin/DeprecationLevel.WARNING)
  @kotlinx/cinterop/ObjCConstructor(designated = true, initSelector = "init")
  public /* secondary */ constructor()

  @kotlinx/cinterop/ObjCConstructor(designated = true, initSelector = "initWithCoder:")
  public /* secondary */ constructor(coder: platform/Foundation/NSCoder)

  @kotlinx/cinterop/ObjCMethod(selector = "date", encoding = "@16@0:8", isStret = false)
  public open external fun date(): platform/Foundation/NSDate

  @kotlinx/cinterop/ObjCMethod(selector = "request", encoding = "@16@0:8", isStret = false)
  public open external fun request(): platform/UserNotifications/UNNotificationRequest

  @kotlin/Deprecated(message = "Use constructor instead", replaceWith = kotlin/ReplaceWith(imports = [], expression = "UNNotification()"), level = kotlin/DeprecationLevel.ERROR)
  @kotlinx/cinterop/ObjCMethod(selector = "init", encoding = "@16@0:8", isStret = false)
  @kotlinx/cinterop/internal/CCall.ConsumesReceiver
  @kotlinx/cinterop/internal/CCall.ReturnsRetained
  @kotlinx/cinterop/ObjCUnavailable
  public open external fun init(): platform/UserNotifications/UNNotification?

  @kotlin/Deprecated(message = "Use constructor instead", replaceWith = kotlin/ReplaceWith(imports = [], expression = "UNNotification(coder)"), level = kotlin/DeprecationLevel.ERROR)
  @kotlinx/cinterop/ObjCMethod(selector = "initWithCoder:", encoding = "@24@0:8@16", isStret = false)
  @kotlinx/cinterop/internal/CCall.ConsumesReceiver
  @kotlinx/cinterop/internal/CCall.ReturnsRetained
  public open external fun initWithCoder(coder: platform/Foundation/NSCoder): platform/UserNotifications/UNNotification?

  @kotlinx/cinterop/ObjCMethod(selector = "copyWithZone:", encoding = "@24@0:8^{_NSZone=}16", isStret = false)
  @kotlinx/cinterop/internal/CCall.ReturnsRetained
  public open external fun copyWithZone(zone: kotlinx/cinterop/CPointer<cnames/structs/_NSZone /* = platform/Foundation/NSZone^ */>?): kotlin/Any

  @kotlinx/cinterop/ObjCMethod(selector = "encodeWithCoder:", encoding = "v24@0:8@16", isStret = false)
  public open external fun encodeWithCoder(coder: platform/Foundation/NSCoder): kotlin/Unit

  public final val date: platform/Foundation/NSDate
    @kotlinx/cinterop/ObjCMethod(selector = "date", encoding = "@16@0:8", isStret = false)
    public final /* non-default */ external get

  public final val request: platform/UserNotifications/UNNotificationRequest
    @kotlinx/cinterop/ObjCMethod(selector = "request", encoding = "@16@0:8", isStret = false)
    public final /* non-default */ external get

  // companion object: Companion

  // nested class: Companion
}

public final companion object platform/UserNotifications/UNNotification.Companion : platform/UserNotifications/UNNotificationMeta, kotlinx/cinterop/ObjCClassOf<platform/UserNotifications/UNNotification> {

  private constructor()
}
