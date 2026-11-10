import { Model } from '../models/model.model';
import { isConnectionGroup } from '../../features/chat/helpers/model-picker.helpers';

/** A background-model select's value for a role switched off. */
export const OFF_VALUE = 'off';

/** How a model is named in a background-model select: with its connection, for a connection's model. */
export function backgroundModelLabel(model: Model): string {
  return isConnectionGroup(model) && model.connection_name ? `${model.display_name} · ${model.connection_name}` : model.display_name;
}
