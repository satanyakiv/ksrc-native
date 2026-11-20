public final enum class platform/UserNotifications/UNShowPreviewsSetting : kotlin/Enum<platform/UserNotifications/UNShowPreviewsSetting>, kotlinx/cinterop/CEnum {

  private constructor(value: kotlin/Long /* = platform/darwin/NSInteger^ */)

  public open val value: kotlin/Long /* = platform/darwin/NSInteger^ */
    public open /* non-default */ get

  // companion object: Companion

  // nested class: Var

  // nested class: Companion

  @kotlinx/cinterop/internal/ConstantValue.Long(value = 0L)
  UNShowPreviewsSettingAlways,

  @kotlinx/cinterop/internal/ConstantValue.Long(value = 1L)
  UNShowPreviewsSettingWhenAuthenticated,

  @kotlinx/cinterop/internal/ConstantValue.Long(value = 2L)
  UNShowPreviewsSettingNever,

  // has Enum.entries
}

public final class platform/UserNotifications/UNShowPreviewsSetting.Var : kotlinx/cinterop/CEnumVar {

  public constructor(rawPtr: kotlin/native/internal/NativePtr /* = kotlinx/cinterop/NativePtr^ */)

  public final var value: platform/UserNotifications/UNShowPreviewsSetting
    public final /* non-default */ external get
    public final /* non-default */ external set(value: platform/UserNotifications/UNShowPreviewsSetting)

  // companion object: Companion

  // nested class: Companion
}

@kotlinx/cinterop/internal/CEnumVarTypeSize(size = 8)
@kotlin/Deprecated(message = "Use sizeOf<T>() or alignOf<T>() instead.", replaceWith = kotlin/ReplaceWith(imports = [], expression = ""), level = kotlin/DeprecationLevel.WARNING)
public final companion object platform/UserNotifications/UNShowPreviewsSetting.Var.Companion : kotlinx/cinterop/CPrimitiveVar.Type {

  private constructor()
}

public final companion object platform/UserNotifications/UNShowPreviewsSetting.Companion {

  private constructor()

  @kotlin/Deprecated(message = "Will be removed.", replaceWith = kotlin/ReplaceWith(imports = [], expression = ""), level = kotlin/DeprecationLevel.WARNING)
  public final fun byValue(value: kotlin/Long /* = platform/darwin/NSInteger^ */): platform/UserNotifications/UNShowPreviewsSetting
}
